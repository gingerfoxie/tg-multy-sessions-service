package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"tg-multy-sessions-service/config"
	pb "tg-multy-sessions-service/internal/pb"
	"tg-multy-sessions-service/internal/server"
	sessionPkg "tg-multy-sessions-service/internal/session"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	sessionManager := sessionPkg.NewSessionManager(cfg.APIID, cfg.APIHash)

	grpcServer := grpc.NewServer()
	telegramServer := server.NewTelegramServer(sessionManager)
	pb.RegisterTelegramServiceServer(grpcServer, telegramServer)

	// Enable reflection for grpcurl
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.AppPort))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	fmt.Printf("Telegram Service listening on port %s\n", cfg.AppPort)

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	<-done
	log.Println("Shutdown signal received")

	log.Println("Stopping gRPC server gracefully...")
	grpcServer.GracefulStop()
	log.Println("gRPC server stopped")

	log.Println("Application stopped gracefully")
}
