package rx_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/virinci/broadauth/internal/rx"
	"github.com/virinci/broadauth/internal/tx"
)

func TestRx(t *testing.T) {
	tx := tx.NewTx(uuid.New())

	go func() {
		for t := range time.Tick(2 * time.Second) {
			fmt.Println("Broadcasting message at ", t)
			tx.Broadcast([]byte("Hello, world!"))
		}
	}()

	rx := rx.NewRx()
	rx.Start()
	defer rx.Close()

	time.Sleep(10 * time.Second)
}
