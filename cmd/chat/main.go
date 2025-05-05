package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/virinci/broadauth/internal/broadcast"
	"github.com/virinci/broadauth/internal/rx"
	"github.com/virinci/broadauth/internal/tx"
)

func main() {
	// Read listen and broadcast ports from environment variables with defaults
	listenPort := os.Getenv("LISTEN_PORT")
	if listenPort == "" {
		listenPort = "8080" // default listen port
	}

	broadcastPort := os.Getenv("BROADCAST_PORT")
	if broadcastPort == "" {
		broadcastPort = "8081" // default broadcast port
	}

	fmt.Printf("Listening on port %s, broadcasting on port %s\n", listenPort, broadcastPort)

	config := broadcast.UDPConfig{
		BroadcastAddr: "255.255.255.255:" + broadcastPort,
		ListenAddr:    ":" + listenPort,
		BufferSize:    1024,
	}

	tx := tx.NewTxWithUDPConfig(uuid.New(), config)
	tx.Start()
	defer tx.Stop()

	rx := rx.NewRxWithUDPConfig(config)
	rx.Start() // Start the listener
	defer rx.Close()

	for {
		fmt.Print("> ")
		os.Stdout.Sync()

		reader := bufio.NewReader(os.Stdin)
		message, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading from stdin:", err)
			continue
		}

		message = strings.TrimSpace(message)

		if message == "" {
			continue
		}

		// Trim whitespace and newline characters
		if message == "/exit" {
			break
		}

		// Broadcast the message
		err = tx.Broadcast([]byte(message))
		if err != nil {
			fmt.Println("Error broadcasting message:", err)
			continue
		}
	}

}
