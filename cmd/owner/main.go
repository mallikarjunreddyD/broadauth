package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/virinci/broadauth/internal/owner"
)

func main() {
	cmAddr := flag.String("cm-addr", "", "CM service address (required)")
	port := flag.Int("port", 0, "TCP listening port (required)")
	hashchainLen := flag.Int("hashchain-len", 512, "Length of hashchains")
	ethURL := flag.String("eth-url", "", "Ethereum node URL (required)")
	contractAddr := flag.String("contract", "", "Contract address (required)")
	privKey := flag.String("private-key", "", "Owner private key in hex format (required)")
	disclosureDelay := flag.Uint64("disclosure-delay", 2, "Disclosure delay for key revelation")

	flag.Parse()

	if *cmAddr == "" || *port == 0 || *ethURL == "" || *contractAddr == "" || *privKey == "" {
		flag.Usage()
		os.Exit(1)
	}

	cfg := owner.Config{
		CMAddr:          *cmAddr,
		TCPAddr:         fmt.Sprintf(":%d", *port),
		HashchainLen:    *hashchainLen,
		EthURL:          *ethURL,
		ContractAddr:    *contractAddr,
		PrivateKey:      *privKey,
		DisclosureDelay: *disclosureDelay,
	}

	o, err := owner.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create owner: %v", err)
	}
	defer o.Close()

	var rcds []uuid.UUID
	for range 20 {
		rcds = append(rcds, o.NewRCD())
	}

	for _, id := range rcds {
		fmt.Println(id)
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- o.ListenAndServe()
	}()

	select {
	case <-shutdown:
		log.Println("Shutting down")
	case err := <-errCh:
		log.Printf("Server error: %v", err)
	}
}
