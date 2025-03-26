package slot

type SlotSource interface {
	GetSlot() (Slot, error)
	Ticker() <-chan Slot
}

type Slot = uint64
