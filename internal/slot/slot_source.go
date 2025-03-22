package slot

type SlotSource interface {
	GetSlot() (Slot, error)
}

type Slot = uint64
