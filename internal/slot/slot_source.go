package slot

import "context"

type SlotSource interface {
	GetSlot() (Slot, error)
	Ticker(ctx context.Context) <-chan Slot
}

type Slot = uint64
