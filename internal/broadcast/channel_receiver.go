package broadcast

import (
	"context"
)

// ChannelReceiver wraps a Receiver to provide a channel-based API
type ChannelReceiver struct {
	receiver Receiver
	messages chan []byte
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewChannelReceiver creates a new channel-based receiver
func NewChannelReceiver(receiver Receiver, bufferSize int) *ChannelReceiver {
	ctx, cancel := context.WithCancel(context.Background())
	cr := &ChannelReceiver{
		receiver: receiver,
		messages: make(chan []byte, bufferSize),
		ctx:      ctx,
		cancel:   cancel,
	}

	receiver.SetMessageHandler(func(data []byte) {
		// Make a copy of the data to prevent issues if the buffer is reused
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)

		// Try to send to channel, drop message if channel is full and non-blocking
		select {
		case cr.messages <- dataCopy:
			// Message sent successfully
		default:
			// Channel is full, message is dropped
		}
	})

	return cr
}

// Messages returns a channel that receives broadcasted messages
func (cr *ChannelReceiver) Messages() <-chan []byte {
	return cr.messages
}

// Start begins listening for broadcasts
func (cr *ChannelReceiver) Start(ctx context.Context) error {
	return cr.receiver.Start(ctx)
}

// Close shuts down the receiver and closes the messages channel
func (cr *ChannelReceiver) Close() error {
	cr.cancel()
	err := cr.receiver.Close()
	close(cr.messages)
	return err
}
