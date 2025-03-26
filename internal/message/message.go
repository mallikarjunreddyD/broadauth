package message

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/virinci/broadauth/internal/slot"
)

// TODO: Implement sending multiple messages in one go.
type Message struct {
	SenderID uuid.UUID
	Slot     slot.Slot
	Kind     MessageKind
	Data     []byte
}

type MessageKind int

const (
	MessageKindHMAC       MessageKind = iota
	MessageKindKeyMessage MessageKind = iota
)

func NewMessage(senderID uuid.UUID, slot slot.Slot, kind MessageKind, data []byte) *Message {
	return &Message{SenderID: senderID, Slot: slot, Kind: kind, Data: data}
}

// Using JSON is implementation detail.
func (m *Message) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *Message) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

func (m *Message) String() string {
	return fmt.Sprintf("Message{SenderID: %s, Slot: %d, Kind: %d, Data: %s}", m.SenderID, m.Slot, m.Kind, m.Data)
}
