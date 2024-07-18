package main

import (
	"fmt"
	"log"
	"math/rand"
	"ssh-realtime/internal/client"
	"sync"
	"time"
)

const (
	numClients    = 5
	numRooms      = 2
	numMessages   = 10
	serverAddress = "localhost:50051"
)

func main() {
	//// Load configuration
	//cfg := config.NewConfig()
	//
	//// Create SSH config (in a real scenario, you'd load this from a file or env vars)
	//sshConfig := &ssh.ClientConfig{
	//	User: "testuser",
	//	Auth: []ssh.AuthMethod{
	//		ssh.Password("testpassword"),
	//	},
	//	HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	//}

	// Create and run clients
	var wg sync.WaitGroup
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			runClient(clientID, serverAddress)
		}(i)
	}

	wg.Wait()
}

func runClient(clientID int, serverAddress string) {
	c, err := client.NewClient(serverAddress, nil, nil) // No etcd endpoints, no SSH config
	if err != nil {
		log.Printf("Client %d: Failed to create client: %v", clientID, err)
		return
	}
	defer c.Close()

	userID := fmt.Sprintf("user%d", clientID)
	roomID := fmt.Sprintf("room%d", clientID%numRooms)

	// Join room
	go func() {
		if err := c.JoinRoom(userID, roomID); err != nil {
			log.Printf("Client %d: Error joining room: %v", clientID, err)
		}
	}()

	// Wait for all clients to join
	time.Sleep(1 * time.Second)

	// Send messages
	for i := 0; i < numMessages; i++ {
		message := fmt.Sprintf("Message %d from %s", i, userID)
		if err := c.SendMessage(userID, roomID, message); err != nil {
			log.Printf("Client %d: Error sending message: %v", clientID, err)
		}
		time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
	}
}
