package slot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	ErrInvalidPollingInterval = errors.New("polling interval must be at least 2 seconds")
	ErrFailedToFetchHeader    = errors.New("failed to fetch block header")
)

// BeaconChainSlotSource implements SlotSource interface using block numbers as slot numbers.
// It polls the blockchain at regular intervals to track slot changes.
type BeaconChainSlotSource struct {
	client        *ethclient.Client
	pollingPeriod time.Duration
}

// NewBeaconChainSlotSource creates a new BeaconChainSlotSource.
// The polling interval must be at least 2 seconds to prevent excessive RPC calls.
func NewBeaconChainSlotSource(client *ethclient.Client, pollingInterval time.Duration) (*BeaconChainSlotSource, error) {
	if pollingInterval < 2*time.Second {
		return nil, fmt.Errorf("%w: got %v", ErrInvalidPollingInterval, pollingInterval)
	}

	return &BeaconChainSlotSource{
		client:        client,
		pollingPeriod: pollingInterval,
	}, nil
}

// GetSlot returns the current slot number, which maps directly to the latest block number.
func (b *BeaconChainSlotSource) GetSlot() (Slot, error) {
	header, err := b.client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrFailedToFetchHeader, err)
	}
	return Slot(header.Number.Uint64()), nil
}

// Ticker returns a channel that emits the current slot number whenever it changes.
// The channel is closed when the provided context is cancelled.
func (b *BeaconChainSlotSource) Ticker(ctx context.Context) <-chan Slot {
	ch := make(chan Slot)
	ticker := time.NewTicker(b.pollingPeriod)

	go func() {
		defer ticker.Stop()
		defer close(ch)

		// Initialize lastSlot to 0 so first actual slot will be emitted
		var lastSlot Slot = 0

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				slot, err := b.GetSlot()
				if err != nil {
					continue // Skip this tick if we can't fetch the slot
				}

				// Only emit if the slot has changed
				if slot != lastSlot {
					select {
					case ch <- slot:
						lastSlot = slot
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch
}
