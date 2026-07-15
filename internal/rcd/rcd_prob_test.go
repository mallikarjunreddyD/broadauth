package rcd

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/google/uuid"
	"github.com/virinci/broadauth/internal/message"
	"github.com/virinci/broadauth/internal/slot"
	"github.com/virinci/broadauth/pkg/hashchain"
)

type mockBroadcaster struct {
	msgs [][]byte
}

func (m *mockBroadcaster) Broadcast(ctx context.Context, data []byte) error {
	cp := make([]byte, len(data))
	copy(cp, data)
	m.msgs = append(m.msgs, cp)
	return nil
}

func (m *mockBroadcaster) Close() error { return nil }

type fakeSlotSource struct{ slotVal uint64 }

func (f *fakeSlotSource) GetSlot() (slot.Slot, error)                 { return slot.Slot(f.slotVal), nil }
func (f *fakeSlotSource) Ticker(ctx context.Context) <-chan slot.Slot { return make(chan slot.Slot) }

func TestFlushBatchProbabilistic(t *testing.T) {
	mb := &mockBroadcaster{}
	fs := &fakeSlotSource{slotVal: 100}

	seed := []byte("unit-test-seed-0123456789")
	hc := hashchain.NewLinear(sha256.New(), seed, 16)

	r := &RCD{
		id:                 uuid.New(),
		broadcaster:        mb,
		slotSource:         fs,
		disclosureMessages: make(chan DisclosurePayload, 10),
		hashChain:          hc,
		hashchainLen:       16,
		cachedKeySlot:      100,
		disclosureDelay:    2,
		mode:               ModeProbabilistic,
		messageBuffer:      make([][]byte, 0),
	}

	// prime cached key
	key := hc.Next()
	copy(r.cachedKey[:], key)

	// Buffer a couple of messages and flush
	if err := r.bufferForBatch([]byte("m1")); err != nil {
		t.Fatalf("buffer failed: %v", err)
	}
	if err := r.bufferForBatch([]byte("m2")); err != nil {
		t.Fatalf("buffer failed: %v", err)
	}

	if err := r.flushBatch(100); err != nil {
		t.Fatalf("flushBatch error: %v", err)
	}

	if len(mb.msgs) == 0 {
		t.Fatalf("expected broadcaster to have messages, got 0")
	}

	var msg message.Message
	if err := msg.Unmarshal(mb.msgs[0]); err != nil {
		t.Fatalf("failed to unmarshal broadcasted message: %v", err)
	}
	if msg.Kind != message.MessageKindHMAC {
		t.Fatalf("expected HMAC message kind, got %v", msg.Kind)
	}

	select {
	case dp := <-r.disclosureMessages:
		if len(dp.Message) == 0 {
			t.Fatalf("expected disclosure payload message (BF), got empty")
		}
		// slot + disclosureDelay + 1: the +1 compensates for
		// broadcastLoop's flushBatch(currentSlot-1) call labeling this
		// batch one tick behind the real time it's actually sent at (see
		// flushBatch's targetSlot comment) - without it, disclosureDelay
		// only ever gets disclosureDelay-1 real ticks of separation.
		if dp.TargetSlot != 103 {
			t.Fatalf("unexpected target slot: %d", dp.TargetSlot)
		}
	default:
		t.Fatalf("expected a disclosure payload queued")
	}
}
