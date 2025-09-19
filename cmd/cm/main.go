package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	contracts "github.com/virinci/broadauth/internal/contract"
)

const (
	addressSize = 20 // 160 bits
	uuidSize    = 16 // 128 bits
	totalSize   = addressSize + uuidSize
)

type response struct {
	Status          string `json:"status"`
	TransactionHash string `json:"transaction_hash"`
	GasUsed         uint64 `json:"gas_used"`
	BlockNumber     uint64 `json:"block_number"`
	Error           string `json:"error,omitempty"`
}

func main() {
	// Parse command line flags
	ethURL := flag.String("eth-url", "", "Ethereum node URL (required)")
	contractAddr := flag.String("contract", "", "Contract address (required)")
	privKey := flag.String("private-key", "", "CM private key in hex format (required)")
	port := flag.Int("port", 0, "TCP listening port (required)")

	flag.Parse()

	// Validate required flags
	if *ethURL == "" || *contractAddr == "" || *privKey == "" || *port == 0 {
		flag.Usage()
		os.Exit(1)
	}

	// Connect to Ethereum node
	client, err := ethclient.Dial(*ethURL)
	if err != nil {
		log.Fatalf("Failed to connect to Ethereum node: %v", err)
	}
	defer client.Close()

	// Set up private key
	privateKey, err := crypto.HexToECDSA(*privKey)
	if err != nil {
		log.Fatalf("Invalid private key: %v", err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Fatal("Cannot assert type: publicKey is not of type *ecdsa.PublicKey")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	log.Printf("CM Address: %s", fromAddress.Hex())

	// Get chain ID
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		log.Fatalf("Failed to get chain ID: %v", err)
	}

	// Create transactor
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		log.Fatalf("Failed to create transactor: %v", err)
	}

	// Initialize contract
	contractAddress := common.HexToAddress(*contractAddr)
	contract, err := contracts.NewContract(contractAddress, client)
	if err != nil {
		log.Fatalf("Failed to instantiate contract: %v", err)
	}

	// Create TCP listener
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to start TCP listener: %v", err)
	}
	defer listener.Close()

	log.Printf("Listening on port %d", *port)

	// Handle graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Start accepting connections
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Failed to accept connection: %v", err)
				continue
			}
			go handleConnection(conn, contract, auth, client)
		}
	}()

	// Wait for shutdown signal
	<-shutdown
	log.Println("Shutting down...")
}

func handleConnection(conn net.Conn, contract *contracts.Contract, auth *bind.TransactOpts, client *ethclient.Client) {
	defer conn.Close()

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	// Read exactly totalSize bytes
	data := make([]byte, totalSize)
	_, err := io.ReadFull(conn, data)
	if err != nil {
		sendResponse(conn, response{
			Status: "failure",
			Error:  fmt.Sprintf("Failed to read data: %v", err),
		})
		return
	}

	// Extract owner address and RCD UUID
	owner := common.BytesToAddress(data[:addressSize])
	var rcdUUID uuid.UUID
	copy(rcdUUID[:], data[addressSize:])

	log.Printf("Registering owner %s with RCD UUID %s", owner.Hex(), rcdUUID)

	// Call RegOwner
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	auth.Context = ctx
	rcdBigInt := new(big.Int).SetBytes(rcdUUID[:])
	tx, err := contract.RegOwner(auth, owner, rcdBigInt)
	if err != nil {
		sendResponse(conn, response{
			Status: "failure",
			Error:  fmt.Sprintf("Failed to call RegOwner: %v", err),
		})
		return
	}

	// Wait for transaction to be mined
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		sendResponse(conn, response{
			Status:          "failure",
			TransactionHash: tx.Hash().Hex(),
			Error:           fmt.Sprintf("Failed to wait for transaction: %v", err),
		})
		return
	}

	// Check transaction status
	if receipt.Status != types.ReceiptStatusSuccessful {
		sendResponse(conn, response{
			Status:          "failure",
			TransactionHash: tx.Hash().Hex(),
			GasUsed:         receipt.GasUsed,
			BlockNumber:     receipt.BlockNumber.Uint64(),
			Error:           "Transaction reverted",
		})
		return
	}

	// Send success response
	sendResponse(conn, response{
		Status:          "success",
		TransactionHash: tx.Hash().Hex(),
		GasUsed:         receipt.GasUsed,
		BlockNumber:     receipt.BlockNumber.Uint64(),
	})
}

func sendResponse(conn net.Conn, resp response) {
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	// Add newline to response for easier parsing
	resp.Error = resp.Error + "\n"

	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		log.Printf("Failed to send response: %v", err)
	}
}
