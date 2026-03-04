package session

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"go.uber.org/zap"

	"github.com/gotd/td/tg"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	apiID    int
	apiHash  string
}

type Session struct {
	ID         string
	mu         sync.RWMutex
	Client     *telegram.Client
	Dispatcher *tg.UpdateDispatcher
	MessageCh  chan *MessageUpdate
	ctx        context.Context
	cancel     context.CancelFunc
	authQRCode string
	isAuthed   bool
	qrReady    chan string
	authDone   chan struct{}
	runStarted sync.Once
	runWG      sync.WaitGroup
}

type MessageUpdate struct {
	MessageID int64  `json:"messageId"`
	From      string `json:"from"`
	SenderID  int64  `json:"senderId"`
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
}

func NewSession(id string, client *telegram.Client, dispatcher *tg.UpdateDispatcher, messageCh chan *MessageUpdate) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		ID:         id,
		Client:     client,
		Dispatcher: dispatcher,
		MessageCh:  messageCh,
		ctx:        ctx,
		cancel:     cancel,
		qrReady:    make(chan string, 1),
		authDone:   make(chan struct{}),
	}
}

func NewSessionManager(apiID int, apiHash string) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		apiID:    apiID,
		apiHash:  apiHash,
	}
}

func (sm *SessionManager) CreateSession(ctx context.Context) (string, string, error) {
	sessionID := fmt.Sprintf("session_%d", time.Now().UnixNano())

	messageCh := make(chan *MessageUpdate, 100)

	d := tg.NewUpdateDispatcher()
	logger, _ := zap.NewDevelopment()

	d.OnNewMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
		if msg, ok := update.Message.(*tg.Message); ok {
			if msg.Out {
				return nil
			}

			from := "Unknown"
			id := int64(0)

			if peer, ok := msg.PeerID.(*tg.PeerUser); ok {
				if user, exists := entities.Users[peer.UserID]; exists {
					from = fmt.Sprintf("%s %s", user.FirstName, user.LastName)
					id = peer.UserID
				}
			} else if peer, ok := msg.PeerID.(*tg.PeerChat); ok {
				if chat, exists := entities.Chats[peer.ChatID]; exists {
					from = chat.Title
					id = peer.ChatID
				}
			} else if peer, ok := msg.PeerID.(*tg.PeerChannel); ok {
				if channel, exists := entities.Channels[peer.ChannelID]; exists {
					from = channel.Title
					id = peer.ChannelID
				}
			}

			msgUpdate := &MessageUpdate{
				MessageID: int64(msg.ID),
				From:      from,
				SenderID:  id,
				Text:      msg.Message,
				Timestamp: int64(msg.Date),
			}

			select {
			case messageCh <- msgUpdate:
				log.Printf("Message sent to channel: %v", msgUpdate.Text)
			default:
				log.Printf("Message channel full, dropping message from %s", from)
			}
		}
		return nil
	})

	client := telegram.NewClient(sm.apiID, sm.apiHash, telegram.Options{
		SessionStorage:   &InMemorySessionStorage{},
		UpdateHandler:    d,
		DialTimeout:      15 * time.Second,
		MigrationTimeout: 30 * time.Second,
		Logger:           logger,
	})

	session := NewSession(sessionID, client, &d, messageCh)

	sm.mu.Lock()
	sm.sessions[sessionID] = session
	sm.mu.Unlock()

	session.startRun()

	select {
	case qrCode := <-session.qrReady:
		log.Printf("QR Code for session %s: %s", sessionID, qrCode)
		return sessionID, qrCode, nil
	case <-session.ctx.Done():
		return "", "", fmt.Errorf("session %s was cancelled during creation", sessionID)
	case <-time.After(30 * time.Second):
		sm.DeleteSession(sessionID)
		return "", "", fmt.Errorf("timeout waiting for QR code for session %s", sessionID)
	}

}

func (s *Session) startRun() {
	s.runStarted.Do(func() {
		s.runWG.Add(1)
		go func() {
			defer s.runWG.Done()
			log.Printf("Starting Run for session %s", s.ID)

			err := s.Client.Run(s.ctx, func(ctx context.Context) error {

				qr := s.Client.QR()

				authFn := func(ctx context.Context, token qrlogin.Token) error {
					qrCodeURL := token.URL()
					log.Printf("Open %s using your phone for session %s", qrCodeURL, s.ID)
					select {
					case s.qrReady <- qrCodeURL:
					default:
						log.Printf("Warning: QR channel busy for session %s", s.ID)
					}
					return nil
				}

				authorization, err := qr.Auth(ctx, qrlogin.OnLoginToken(s.Dispatcher), authFn)
				if err != nil {
					log.Printf("Auth error for session %s: %v", s.ID, err)
					return err
				}

				u, ok := authorization.User.AsNotEmpty()
				if !ok {
					err := fmt.Errorf("unexpected type %T", authorization.User)
					return err
				}

				log.Printf("✅ Auth successful for session %s. User: %s (ID: %d)", s.ID, u.Username, u.ID)
				s.isAuthed = true

				close(s.authDone)

				log.Printf("Run loop active for session %s. Waiting for context cancellation.", s.ID)
				<-s.ctx.Done()
				log.Printf("Run ended for session %s", s.ID)
				return nil
			})

			if err != nil {
				log.Printf("Run ended with error for session %s: %v", s.ID, err)
			} else {
				log.Printf("Run ended normally for session %s", s.ID)
			}
		}()
	})
}

func (sm *SessionManager) startAuth(session *Session) (string, error) {
	log.Printf("Starting auth for session %s", session.ID)

	d := tg.NewUpdateDispatcher()

	qrChan := make(chan string, 1)
	errChan := make(chan error, 1)

	err := session.Client.Run(session.ctx, func(ctx context.Context) error {
		qr := session.Client.QR()

		authFn := func(ctx context.Context, token qrlogin.Token) error {
			qrCodeURL := token.URL()
			log.Printf("Open %s using your phone for session %s", qrCodeURL, session.ID)
			select {
			case qrChan <- qrCodeURL:
			default:
			}
			return nil
		}

		authorization, err := qr.Auth(ctx, qrlogin.OnLoginToken(d), authFn)
		if err != nil {
			log.Printf("Auth error for session %s: %v", session.ID, err)
			errChan <- err
			return err
		}

		u, ok := authorization.User.AsNotEmpty()
		if !ok {
			err := fmt.Errorf("unexpected type %T", authorization.User)
			errChan <- err
			return err
		}

		log.Printf("✅ Auth successful for session %s. User: %s (ID: %d)", session.ID, u.Username, u.ID)
		session.isAuthed = true

		return nil
	})

	if err != nil {
		return "", err
	}

	select {
	case qrURL := <-qrChan:
		return qrURL, nil
	case err := <-errChan:
		return "", err
	case <-time.After(30 * time.Second):
		return "", fmt.Errorf("timeout waiting for first QR code")
	}

}

func (sm *SessionManager) GetSession(sessionID string) (*Session, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, status.Error(codes.NotFound, "session not found")
	}

	return session, nil
}

func (sm *SessionManager) SendMessage(sessionID, peerStr, text string) (int64, error) {
	session, err := sm.GetSession(sessionID)
	if err != nil {
		return 0, err
	}

	if !session.isAuthed {
		return 0, status.Error(codes.FailedPrecondition, "session not authenticated")
	}

	userID, err := strconv.ParseInt(peerStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid peer format, expected numeric ID or username: %w", err)
	}

	users, err := session.Client.API().UsersGetUsers(session.ctx, []tg.InputUserClass{&tg.InputUser{UserID: int64(userID), AccessHash: 0}})
	if err != nil {
		return 0, fmt.Errorf("failed to get user info: %w", err)
	}

	if len(users) == 0 {
		return 0, fmt.Errorf("user not found")
	}

	user, ok := users[0].(*tg.User)
	if !ok || user == nil {
		return 0, fmt.Errorf("user not found or invalid")
	}

	inputPeer := &tg.InputPeerUser{
		UserID:     user.ID,
		AccessHash: user.AccessHash,
	}

	result, err := session.Client.API().MessagesSendMessage(session.ctx, &tg.MessagesSendMessageRequest{
		Peer:     inputPeer,
		Message:  text,
		RandomID: time.Now().UnixNano(),
	})

	if err != nil {
		return 0, err
	}

	if updates, ok := result.(*tg.Updates); ok && len(updates.Updates) > 0 {

		switch update := updates.Updates[0].(type) {
		case *tg.UpdateNewMessage:
			if msg, ok := update.Message.(*tg.Message); ok {
				return int64(msg.ID), nil
			}
		case *tg.UpdateMessageID:
			return int64(update.ID), nil
		default:
			log.Printf("SendMessage: Unexpected update type: %T", update)
		}
	}

	return 0, fmt.Errorf("could not extract message ID from response")
}

func (sm *SessionManager) SubscribeToMessages(sessionID string) (<-chan *MessageUpdate, error) {
	sm.mu.RLock()
	session, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !exists {
		return nil, status.Error(codes.NotFound, "session not found")
	}

	ch := session.MessageCh

	return ch, nil
}

func (sm *SessionManager) DeleteSession(sessionID string) error {
	sm.mu.Lock()
	session, exists := sm.sessions[sessionID]
	if !exists {
		sm.mu.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	log.Printf("Logging out session %s from Telegram server...", sessionID)
	_, err := session.Client.API().AuthLogOut(session.ctx)
	if err != nil {
		log.Printf("Warning: Failed to logout session %s from Telegram:", err)
	} else {
		log.Printf("Successfully logged out session %s from Telegram server", sessionID)
	}

	session.cancel()

	session.runWG.Wait()

	log.Printf("Session %s deleted", sessionID)
	return nil
}

type InMemorySessionStorage struct{}

func (i *InMemorySessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	return nil, nil // For simplicity, we don't persist sessions
}

func (i *InMemorySessionStorage) StoreSession(ctx context.Context, data []byte) error {
	return nil // For simplicity, we don't persist sessions
}
