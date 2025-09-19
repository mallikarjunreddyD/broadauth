package slot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

const ethURL = "http://localhost:8545"

func TestBeaconChainSlotSource_GetSlot(t *testing.T) {
	client, err := ethclient.Dial(ethURL)
	if err != nil {
		t.Skipf("Skipping test: failed to connect to eth client at %s: %v", ethURL, err)
		return
	}
	defer client.Close()

	source, err := NewBeaconChainSlotSource(client, 3*time.Second)
	if err != nil {
		t.Fatalf("Failed to create slot source: %v", err)
	}

	slot, err := source.GetSlot()
	if err != nil {
		t.Fatalf("Failed to get slot: %v", err)
	}

	fmt.Printf("Current slot (block) number: %d\n", slot)
}

func TestBeaconChainSlotSource_Ticker(t *testing.T) {
	client, err := ethclient.Dial(ethURL)
	if err != nil {
		t.Skipf("Skipping test: failed to connect to eth client at %s: %v", ethURL, err)
		return
	}
	defer client.Close()

	source, err := NewBeaconChainSlotSource(client, 3*time.Second)
	if err != nil {
		t.Fatalf("Failed to create slot source: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch := source.Ticker(ctx)

	fmt.Println("Waiting for slot updates (10 seconds)...")
	var lastSlot Slot
	var updates int

	for slot := range ch {
		updates++
		fmt.Printf("Received slot update: %d (delta: %d)\n", slot, slot-lastSlot)
		lastSlot = slot
	}

	fmt.Printf("Received %d slot updates\n", updates)
	if updates == 0 {
		t.Error("Expected at least one slot update but got none")
	}
}

func contains(s, substr string) bool {
	return s != "" && substr != "" && s != substr && len(s) > len(substr) && s[len(s)-len(substr):] == substr
}
