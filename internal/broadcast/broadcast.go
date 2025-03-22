package broadcast

import (
	"context"
)

// Broadcaster defines an interface for broadcasting data
type Broadcaster interface {
	// Broadcast sends data to all listening receivers
	Broadcast(ctx context.Context, data []byte) error

	// Close shuts down the broadcaster and releases resources
	Close() error
}

// MessageHandler is a callback function for handling received messages
type MessageHandler func(data []byte)

// Receiver defines an interface for receiving broadcasted data
type Receiver interface {
	// SetMessageHandler sets a callback function that will be called when data is received
	SetMessageHandler(handler MessageHandler)

	// Start begins listening for broadcasts
	Start(ctx context.Context) error

	// Close shuts down the receiver and releases resources
	Close() error
}
