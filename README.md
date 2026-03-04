# Telegram Multi-Session Service

A Go service that establishes and maintains multiple independent connections to Telegram through the gotd library. The service allows dynamic creation and deletion of connections, sending text messages, and receiving text messages via gRPC interface.

## Features

- Dynamic creation and deletion of Telegram connections
- Send and receive text messages
- Isolated connections - problems with one connection don't affect others
- gRPC interface for external communication
- In-memory session state storage
- Environment-based configuration using Viper
- Graceful shutdown handling
- QR code authentication for new sessions

## Prerequisites

- Go 1.26
- Telegram API credentials (obtain from [my.telegram.org](https://my.telegram.org))

## Installation and Setup

### 1. Clone the repository

```bash
git clone <repository-url>
cd tg-multy-sessions-service
```

### 2. Install dependencies

```bash
go mod tidy
```

### 3. Configure environment variables

Create a `.env` file in the root directory with the following content:

```bash
APP_PORT=50051
TELEGRAM_API_ID=your_api_id_here
TELEGRAM_API_HASH=your_api_hash_here
```

Replace `your_api_id_here` and `your_api_hash_here` with your actual Telegram API credentials. Alternatively, you can copy the provided `.env.example` file:

```bash
cp .env.example .env
```

Then edit the `.env` file with your actual values.

### 4. Build and run

```bash
go build -o telegram-service .
./telegram-service
```

Or run directly:

```bash
go run main.go
```

## Usage Examples

### Using grpcurl

First, install grpcurl:

```bash
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
```

#### Create a session:

```bash
grpcurl -plaintext -d '{}' localhost:50051 pact.telegram.TelegramService.CreateSession
```

This will return a session ID and QR code URL. Open the QR code URL on your phone using Telegram (Settings → Devices → Scan QR) to authenticate.

#### Send a message (after authentication):

```bash
grpcurl -plaintext -d '{"sessionId": "session_123456789", "peer": "123456789", "text": "Hello from gRPC!"}' localhost:50051 pact.telegram.TelegramService.SendMessage
```

Replace the peer ID with the actual user ID you want to send a message to.

#### Subscribe to messages:

```bash
grpcurl -plaintext -d '{"sessionId": "session_123456789"}' localhost:50051 pact.telegram.TelegramService.SubscribeMessages
```

#### Delete a session:

```bash
grpcurl -plaintext -d '{"sessionId": "session_123456789"}' localhost:50051 pact.telegram.TelegramService.DeleteSession
```

## Architecture

### Components

1. **Main Application (`main.go`)**:
   - Initializes the gRPC server
   - Sets up graceful shutdown handling
   - Creates the session manager with configuration

2. **Configuration (`config/config.go`)**:
   - Uses Viper for environment variable management
   - Loads configuration from `.env` file
   - Provides default values for missing configuration options

3. **gRPC Server (`internal/server/server.go`)**:
   - Implements the TelegramService interface
   - Handles all gRPC method calls
   - Delegates operations to the session manager

4. **Session Manager (`internal/session/manager.go`)**:
   - Manages multiple Telegram sessions
   - Handles session lifecycle (creation, deletion)
   - Provides thread-safe access to sessions
   - Coordinates message handling between sessions

5. **Session (`internal/session/manager.go`)**:
   - Represents a single Telegram connection
   - Manages the Telegram client instance
   - Handles authentication flow
   - Processes incoming messages

### Key Design Decisions

- **Independent Sessions**: Each session runs independently with its own client
- **Thread Safety**: Proper synchronization using mutexes to ensure safe concurrent access
- **Graceful Shutdown**: The service handles interrupt signals and stops gracefully
- **Memory Storage**: Session states are stored in memory for simplicity (suitable for testing)
- **Error Handling**: Comprehensive error handling with appropriate gRPC status codes
- **Resource Management**: Proper cleanup of resources when sessions are deleted

### Security Considerations

- Store API credentials securely
- Sessions are in-memory only (no persistence)
- Each session is isolated from others
- Authentication is handled through Telegram's secure QR code system

## Development

To regenerate protobuf files if needed:

```bash
protoc --go_out=. --go-grpc_out=. proto/telegram.proto
```

## Troubleshooting

- Ensure your Telegram API credentials are correct
- Make sure the port specified in `APP_PORT` is available
- Check logs for detailed error information
- Verify that the Telegram account isn't restricted or banned

