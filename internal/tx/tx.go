package tx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"

	"github.com/google/uuid"
	"github.com/virinci/broadauth/internal/broadcast"
	"github.com/virinci/broadauth/internal/message"
	"github.com/virinci/broadauth/internal/slot"
	"github.com/virinci/broadauth/pkg/hashchain"
)

type Tx struct {
	ID          uuid.UUID
	broadcaster broadcast.Broadcaster
	hashchain   hashchain.HashChain
	slotSource  slot.SlotSource
}

func NewTx(id uuid.UUID) *Tx {
	broadcaster, err := broadcast.NewUDPBroadcaster(broadcast.DefaultUDPConfig())
	if err != nil {
		panic(err)
	}
	linearHashchain := hashchain.NewLinear(sha256.New(), []byte("test"), 100)
	slotSource := slot.NewUnixEpochSlotSource(2000)
	return &Tx{ID: id, broadcaster: broadcaster, hashchain: linearHashchain, slotSource: slotSource}
}

func (t *Tx) Broadcast(data []byte) error {
	// TODO: Get the key at the current slot, instead of naively using the next key.
	key := t.hashchain.Next()

	signature := HMAC(key, data)

	slot, err := t.slotSource.GetSlot()
	if err != nil {
		return err
	}

	message := message.NewMessage(t.ID, slot, message.MessageKindHMAC, signature)

	data, err = message.Marshal()
	if err != nil {
		return err
	}

	err = t.broadcaster.Broadcast(context.Background(), data)
	if err != nil {
		return err
	}

	// TODO: Add the key and the message to a broadcast queue.
	return nil
}

func HMAC(key []byte, data []byte) []byte {
	hmac := hmac.New(sha256.New, key)
	hmac.Write(data)
	return hmac.Sum(nil)
}
