package broadcast_test

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"testing"
	"time"

	"github.com/virinci/broadauth/internal/broadcast"
)

func TestUDPBroadcasterReceiver(t *testing.T) {
	// Create a UDP broadcaster
	config := broadcast.DefaultUDPConfig()
	broadcaster, err := broadcast.NewUDPBroadcaster(config)
	if err != nil {
		fmt.Printf("Failed to create broadcaster: %v\n", err)
		return
	}
	defer broadcaster.Close()

	// Create UDP receivers
	receiver, err := broadcast.NewUDPReceiver(config)
	if err != nil {
		fmt.Printf("Failed to create receiver: %v\n", err)
		return
	}

	// Option 1: Use callback-based API
	receiver.SetMessageHandler(func(data []byte) {
		fmt.Printf("Received message: %s\n", string(data))
	})

	// Option 2: Use channel-based API
	channelReceiver := broadcast.NewChannelReceiver(receiver, 10)
	go func() {
		for msg := range channelReceiver.Messages() {
			fmt.Printf("Received via channel: %s\n", string(msg))
		}
	}()

	// Start the receiver
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := channelReceiver.Start(ctx); err != nil {
		fmt.Printf("Failed to start receiver: %v\n", err)
		return
	}
	defer channelReceiver.Close()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	// Broadcast a message every second
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	after := time.After(1100 * time.Millisecond)

	for {
		select {
		case <-ticker.C:
			message := fmt.Sprintf("Hello, world! Time: %s", time.Now().Format(time.RFC3339))
			if err := broadcaster.Broadcast(ctx, []byte(message)); err != nil {
				fmt.Printf("Error broadcasting: %v\n", err)
			}
		case <-after:
			fmt.Println("Shutting down after test duration...")
			return
		case <-sigCh:
			fmt.Println("Shutting down after SIGINT...")
			return
		}
	}
}
