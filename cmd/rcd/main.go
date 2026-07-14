package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
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
	modeStr := flag.String("mode", "", "Operation mode: 'deterministic' or 'probabilistic'")

	bench := flag.Bool("bench", false, "Enable runtime benchmarking metrics")

	adaptive := flag.Bool("adaptive", false, "Enable Prob-Adaptive slot timing (EIP-1559-style queue-utilization controller)")
	tMin := flag.Uint64("tmin", 1000, "Adaptive mode: minimum slot duration in ms")
	tMax := flag.Uint64("tmax", 8000, "Adaptive mode: maximum slot duration in ms")
	msgRate := flag.Int("msg-rate", 50, "Traffic generator rate in packets/sec")

	flag.Parse()

	if *uuidStr == "" || *ownerAddr == "" || *ethURL == "" || *contractAddr == "" {
		flag.Usage()
		os.Exit(1)
	}

	id, err := uuid.Parse(*uuidStr)
	if err != nil {
		log.Fatalf("Invalid UUID: %v", err)
	}

	var mode rcd.Mode
	switch strings.ToLower(*modeStr) {
	case "deterministic", "det", "":
		mode = rcd.ModeDeterministic
		log.Println("Starting RCD in DETERMINISTIC mode")
	case "probabilistic", "prob":
		mode = rcd.ModeProbabilistic
		log.Println("Starting RCD in PROBABILISTIC mode")
	default:
		log.Fatalf("Unknown mode: %s", *modeStr)
	}

	cfg := rcd.Config{
		UUID:               id,
		OwnerAddr:          *ownerAddr,
		EthURL:             *ethURL,
		ContractAddr:       *contractAddr,
		HashchainLen:       *hashchainLen,
		DisclosureDelay:    *disclosureDelay,
		SimulationTime:     *simulationTime,
		Mode:               mode,
		EnableBenchmarking: *bench, // Pass the flag value
		Adaptive:           *adaptive,
		TMin:               *tMin,
		TMax:               *tMax,
		MessageRate:        *msgRate,
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
