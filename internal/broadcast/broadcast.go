package broadcast

import (
	"context"
)

// Broadcaster defines an interface for broadcasting data
type Broadcaster interface {
	// Broadcast sends data over the throttled auth channel: it is rate-limited
	// to RadioBytesPerSec so the disclosure/HMAC pipeline is the bottleneck the
	// adaptive controller relieves. Used for HMACs and key disclosures.
	Broadcast(ctx context.Context, data []byte) error

	// BroadcastUnthrottled sends data over the application plane with no rate
	// limit. Application data is not charged against the auth-channel budget,
	// so a high message rate cannot starve the disclosure pipeline. Used for
	// raw Data messages.
	BroadcastUnthrottled(ctx context.Context, data []byte) error

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
