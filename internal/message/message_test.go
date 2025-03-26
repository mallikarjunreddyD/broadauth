package message_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/virinci/broadauth/internal/message"
)

func TestMessage(t *testing.T) {
	message := message.NewMessage(uuid.New(), 1, message.MessageKindKeyMessage, []byte("test"))
	fmt.Println(message)
}

func TestMessageMarshal(t *testing.T) {
	message := message.NewMessage(uuid.New(), 1, message.MessageKindKeyMessage, []byte("test"))
	data, err := message.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal message: %v", err)
	}
	fmt.Println(string(data))
}

func TestMessageUnmarshal(t *testing.T) {
	messageForMarshal := message.NewMessage(uuid.New(), 1, message.MessageKindKeyMessage, []byte("test"))
	data, err := messageForMarshal.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal message: %v", err)
	}
	unmarshalledMessage := &message.Message{}
	err = unmarshalledMessage.Unmarshal(data)
	if err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}
	if unmarshalledMessage.SenderID != messageForMarshal.SenderID {
		t.Fatalf("sender ID does not match")
	}
	if unmarshalledMessage.Slot != messageForMarshal.Slot {
		t.Fatalf("slot does not match")
	}
	if unmarshalledMessage.Kind != messageForMarshal.Kind {
		t.Fatalf("kind does not match")
	}
	if !bytes.Equal(unmarshalledMessage.Data, messageForMarshal.Data) {
		t.Fatalf("data does not match")
	}
	fmt.Println(unmarshalledMessage)
}

func TestMessageString(t *testing.T) {
	message := message.NewMessage(uuid.New(), 1, message.MessageKindKeyMessage, []byte("test"))
	fmt.Println(message.String())
}
