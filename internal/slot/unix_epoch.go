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
