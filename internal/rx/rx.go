package rx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/virinci/broadauth/internal/broadcast"
	"github.com/virinci/broadauth/internal/message"
	"github.com/virinci/broadauth/internal/slot"
)

type Rx struct {
	slotSource slot.SlotSource
	receiver   broadcast.Receiver

	// TODO: Use a sophisticated key store with TTL and auto key expiration.
	receivedHMACs sync.Map
}

func NewRx() *Rx {
	receiver, err := broadcast.NewUDPReceiver(broadcast.DefaultUDPConfig())
	if err != nil {
		panic(err)
	}
	// TODO: Make this configurable.
	slotSource := slot.NewUnixEpochSlotSource(2000)
	return &Rx{slotSource: slotSource, receiver: receiver}
}

func (r *Rx) Start() error {
	r.receiver.SetMessageHandler(func(data []byte) {
		receivedMessage := &message.Message{}
		err := receivedMessage.Unmarshal(data)
		if err != nil {
			fmt.Println("Error unmarshalling message: ", err)
			return
		}

		if receivedMessage.Kind == message.MessageKindKeyMessage {
			if r.VerifyKeyMessage(receivedMessage) {
				fmt.Println("Key message verified")
			} else {
				fmt.Println("Key message verification failed")
			}
			fmt.Println("Message: ", string(receivedMessage.Data[32:]))
		} else {
			fmt.Println("HMAC: ", receivedMessage.Data)

			// TODO: Verify if the HMAC is of correct length.
			var keyArray [32]byte
			copy(keyArray[:], receivedMessage.Data[:32])
			r.receivedHMACs.Store(keyArray, true)
		}
	})

	return r.receiver.Start(context.Background())
}

func (r *Rx) Close() error {
	return r.receiver.Close()
}

func (r *Rx) VerifyKeyMessage(receivedMessage *message.Message) bool {
	if receivedMessage.Kind != message.MessageKindKeyMessage {
		return false
	}

	// Calculate the HMAC of the payload.
	key, payload := receivedMessage.Data[:32], receivedMessage.Data[32:]
	fmt.Println("Verifying key message with key: ", key)

	hmac := hmac.New(sha256.New, key)
	hmac.Write(payload)
	signature := hmac.Sum(nil)

	// Make the signature a fixed size array so it can be used as a key in the sync.Map.
	var signatureArray [32]byte
	copy(signatureArray[:], signature)

	// Check if the signature has been received before.
	_, ok := r.receivedHMACs.Load(signatureArray)
	if ok {
		r.receivedHMACs.Delete(signatureArray)
	}
	return ok
}
