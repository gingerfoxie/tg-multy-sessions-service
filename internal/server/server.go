package server

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "tg-multy-sessions-service/internal/pb"
	sessionPkg "tg-multy-sessions-service/internal/session"
)

type TelegramServer struct {
	pb.UnimplementedTelegramServiceServer
	sessionManager *sessionPkg.SessionManager
}

func NewTelegramServer(sessionManager *sessionPkg.SessionManager) *TelegramServer {
	return &TelegramServer{
		sessionManager: sessionManager,
	}
}

func (s *TelegramServer) CreateSession(ctx context.Context, req *pb.CreateSessionRequest) (*pb.CreateSessionResponse, error) {
	sessionID, qrCode, err := s.sessionManager.CreateSession(ctx)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		return nil, status.Error(codes.Internal, "failed to create session")
	}

	return &pb.CreateSessionResponse{
		SessionId: &sessionID,
		QrCode:    &qrCode,
	}, nil
}

func (s *TelegramServer) DeleteSession(ctx context.Context, req *pb.DeleteSessionRequest) (*pb.DeleteSessionResponse, error) {
	err := s.sessionManager.DeleteSession(*req.SessionId)
	if err != nil {
		log.Printf("Failed to delete session: %v", err)
		return nil, err
	}

	return &pb.DeleteSessionResponse{}, nil
}

func (s *TelegramServer) SendMessage(ctx context.Context, req *pb.SendMessageRequest) (*pb.SendMessageResponse, error) {
	msgID, err := s.sessionManager.SendMessage(*req.SessionId, *req.Peer, *req.Text)
	if err != nil {
		log.Printf("Failed to send message: %v", err)
		return nil, err
	}

	return &pb.SendMessageResponse{
		MessageId: &msgID,
	}, nil
}

func (s *TelegramServer) SubscribeMessages(req *pb.SubscribeMessagesRequest, stream pb.TelegramService_SubscribeMessagesServer) error {
	sessionID := req.SessionId

	msgCh, err := s.sessionManager.SubscribeToMessages(*sessionID)
	if err != nil {
		return err
	}

	for {
		select {
		case msg := <-msgCh:
			from := fmt.Sprintf("%s (%d)", msg.From, msg.SenderID)
			pbMsg := &pb.MessageUpdate{
				MessageId: &msg.MessageID,
				From:      &from,
				Text:      &msg.Text,
				Timestamp: &msg.Timestamp,
			}
			if err := stream.Send(pbMsg); err != nil {
				log.Printf("Error sending message to stream for session %s: %v", sessionID, err)
			}
		case <-stream.Context().Done():
			log.Printf("Client disconnected from SubscribeMessages for session %s", sessionID)
			return nil
		}
	}
}
