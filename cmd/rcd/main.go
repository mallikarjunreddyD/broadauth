package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/virinci/broadauth/internal/rcd"
)

func main() {
	uuidStr := flag.String("uuid", "", "RCD UUID (required)")
	ownerAddr := flag.String("owner-addr", "", "Owner's network address (required)")
	ethURL := flag.String("eth-url", "", "Ethereum node URL (required)")
	contractAddr := flag.String("contract", "", "Contract address (required)")
	hashchainLen := flag.Int("hashchain-len", 1024, "Length of hashchains")
	disclosureDelay := flag.Uint64("disclosure-delay", 2, "Disclosure delay for key revelation")
	simulationTime := flag.Duration("simulation-time", 3*time.Minute, "Duration to run the simulation")

	flag.Parse()

	if *uuidStr == "" || *ownerAddr == "" || *ethURL == "" || *contractAddr == "" {
		flag.Usage()
		os.Exit(1)
	}

	id, err := uuid.Parse(*uuidStr)
	if err != nil {
		log.Fatalf("Invalid UUID: %v", err)
	}

	cfg := rcd.Config{
		UUID:            id,
		OwnerAddr:       *ownerAddr,
		EthURL:          *ethURL,
		ContractAddr:    *contractAddr,
		HashchainLen:    *hashchainLen,
		DisclosureDelay: *disclosureDelay,
		SimulationTime:  *simulationTime,
	}

	r, err := rcd.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create RCD: %v", err)
	}

	if err := r.Start(); err != nil {
		log.Fatalf("Failed to start RCD: %v", err)
	}

	// Wait for simulation duration
	simTimer := time.NewTimer(cfg.SimulationTime)
	defer simTimer.Stop()

	// Wait for either simulation completion or interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	select {
	case <-simTimer.C:
		log.Printf("Simulation time completed")
	case sig := <-sigChan:
		log.Printf("Received signal %v, stopping simulation", sig)
	}

	// Stop simulation and cleanup
	if err := r.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}
}
