package main

import (
	"log"
	"ssh-realtime/internal/server"
)

func main() {
	rabbitMQURL := "amqp://guest:guest@localhost:5672/"
	if err := server.RunServer(":50051", []string{"localhost:2379"}, rabbitMQURL); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}
