package slot

import "time"

type UnixEpochSlotSource struct {
	durationMilli uint64
}

func NewUnixEpochSlotSource(durationMilli uint64) *UnixEpochSlotSource {
	return &UnixEpochSlotSource{durationMilli}
}

func (s *UnixEpochSlotSource) GetSlot() (Slot, error) {
	return Slot(uint64(time.Now().UnixMilli()) / s.durationMilli), nil
}

func (s *UnixEpochSlotSource) Ticker() <-chan Slot {
	ticker := time.NewTicker(time.Duration(s.durationMilli) * time.Millisecond)
	ch := make(chan Slot)
	go func() {
		for range ticker.C {
			ch <- Slot(uint64(time.Now().UnixMilli()) / s.durationMilli)
		}
	}()
	return ch
}
