package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"ssh-realtime/internal/client"
	"strings"
)

func main() {
	c, err := client.NewClient("localhost:50051", nil, nil)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter your user ID: ")
	userId, _ := reader.ReadString('\n')
	userId = strings.TrimSpace(userId)

	fmt.Print("Enter room ID to join: ")
	roomId, _ := reader.ReadString('\n')
	roomId = strings.TrimSpace(roomId)

	go func() {
		if err := c.JoinRoom(userId, roomId); err != nil {
			log.Printf("Error joining room: %v", err)
		}
	}()

	for {
		fmt.Print("Enter message (or 'quit' to exit): ")
		msg, _ := reader.ReadString('\n')
		msg = strings.TrimSpace(msg)

		if msg == "quit" {
			break
		}

		if err := c.SendMessage(userId, roomId, msg); err != nil {
			log.Printf("Error sending message: %v", err)
		}
	}
}
