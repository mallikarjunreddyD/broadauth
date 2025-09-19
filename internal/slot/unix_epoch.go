package slot

import (
	"context"
	"time"
)

type UnixEpochSlotSource struct {
	durationMilli uint64
}

func NewUnixEpochSlotSource(durationMilli uint64) *UnixEpochSlotSource {
	return &UnixEpochSlotSource{durationMilli}
}

func (s *UnixEpochSlotSource) GetSlot() (Slot, error) {
	return Slot(uint64(time.Now().UnixMilli()) / s.durationMilli), nil
}

func (s *UnixEpochSlotSource) Ticker(ctx context.Context) <-chan Slot {
	ticker := time.NewTicker(time.Duration(s.durationMilli) * time.Millisecond)
	ch := make(chan Slot)

	go func() {
		defer ticker.Stop()
		defer close(ch)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				slot := Slot(uint64(time.Now().UnixMilli()) / s.durationMilli)
				select {
				case ch <- slot:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch
}
