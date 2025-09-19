// Package tx is deprecated. Use the RCD implementation instead.
// This package will be removed in a future release.
package tx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"
	"github.com/virinci/broadauth/internal/broadcast"
	"github.com/virinci/broadauth/internal/message"
	"github.com/virinci/broadauth/internal/slot"
	"github.com/virinci/broadauth/pkg/hashchain"
)

// Deprecated: Use RCD implementation instead.
type Tx struct {
	ID          uuid.UUID
	broadcaster broadcast.Broadcaster
	hashchain   hashchain.HashChain

	slotSource      slot.SlotSource
	disclosureDelay uint64

	ctx    context.Context
	cancel context.CancelFunc

	disclosureMessages chan DisclosurePayload
}

type DisclosurePayload struct {
	Message    []byte
	Key        [32]byte
	TargetSlot uint64
}

// Deprecated: Use RCD implementation instead.
func NewTxWithUDPConfig(id uuid.UUID, config broadcast.UDPConfig) *Tx {
	broadcaster, err := broadcast.NewUDPBroadcaster(config)
	if err != nil {
		panic(err)
	}
	linearHashchain := hashchain.NewLinear(sha256.New(), []byte("test"), 100)
	slotSource := slot.NewUnixEpochSlotSource(2000)
	return &Tx{
		ID:                 id,
		broadcaster:        broadcaster,
		hashchain:          linearHashchain,
		slotSource:         slotSource,
		disclosureDelay:    3,
		disclosureMessages: make(chan DisclosurePayload, 1024),
	}
}

// Deprecated: Use RCD implementation instead.
func NewTx(id uuid.UUID) *Tx {
	return NewTxWithUDPConfig(id, broadcast.DefaultUDPConfig())
}

func (t *Tx) Start() error {
	t.ctx, t.cancel = context.WithCancel(context.Background())
	go t.DisclosureBroadcastWorker()
	return nil
}

func (t *Tx) Stop() error {
	t.cancel()
	return nil
}

func (t *Tx) Broadcast(data []byte) error {
	// Get current slot and key
	currentSlot, err := t.slotSource.GetSlot()
	if err != nil {
		return err
	}

	key := t.hashchain.Next()
	if len(key) == 0 {
		return fmt.Errorf("no more keys available in hashchain")
	}

	fmt.Println("Broadcasting with key: ", key)

	// Calculate HMAC and broadcast it
	signature := HMAC(key, data)
	hmacMessage := message.NewMessage(t.ID, currentSlot, message.MessageKindHMAC, signature)

	hmacData, err := hmacMessage.Marshal()
	if err != nil {
		return err
	}

	if err := t.broadcaster.Broadcast(context.Background(), hmacData); err != nil {
		return err
	}

	// Queue disclosure for later broadcast
	targetSlot := currentSlot + t.disclosureDelay

	keyArray := [32]byte{}
	copy(keyArray[:], key)
	select {
	case t.disclosureMessages <- DisclosurePayload{
		Message:    data,
		Key:        keyArray,
		TargetSlot: targetSlot,
	}:
	default:
		return fmt.Errorf("disclosure message queue is full")
	}
	return nil
}

func (t *Tx) DisclosureBroadcastWorker() {
	ticker := t.slotSource.Ticker()
	pendingDisclosures := make([]DisclosurePayload, 0)

	for {
		select {
		case currentSlot := <-ticker:
			// Process any new disclosure messages
			for {
				select {
				case disclosure := <-t.disclosureMessages:
					pendingDisclosures = append(pendingDisclosures, disclosure)
				default:
					goto ProcessPending
				}
			}

		ProcessPending:
			// Check and broadcast ready disclosures
			readyIdx := 0
			for i, disclosure := range pendingDisclosures {
				if disclosure.TargetSlot > currentSlot {
					// Keep this and remaining disclosures for later
					readyIdx = i
					break
				}

				// Create and broadcast disclosure message
				disclosureMsg := message.NewMessage(
					t.ID,
					currentSlot,
					message.MessageKindKeyMessage,
					append(disclosure.Key[:], disclosure.Message...),
				)

				data, err := disclosureMsg.Marshal()
				if err != nil {
					fmt.Printf("Error marshalling disclosure message: %v\n", err)
					continue
				}

				if err := t.broadcaster.Broadcast(t.ctx, data); err != nil {
					fmt.Printf("Error broadcasting disclosure message: %v\n", err)
					continue
				}
				readyIdx = i + 1
			}

			// Remove processed disclosures
			if readyIdx > 0 {
				pendingDisclosures = pendingDisclosures[readyIdx:]
			}

		case <-t.ctx.Done():
			return
		}
	}
}

func HMAC(key []byte, data []byte) []byte {
	hmac := hmac.New(sha256.New, key)
	hmac.Write(data)
	return hmac.Sum(nil)
}
