package contract_test

import (
	// "context"
	"log"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/virinci/broadauth/internal/contract"
)

func TestSimulated(t *testing.T) {
	// Create private key for test account
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Create auth
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, big.NewInt(1337))
	if err != nil {
		t.Fatalf("Failed to create auth: %v", err)
	}

	// Fund account
	balance := new(big.Int)
	balance.Exp(big.NewInt(10), big.NewInt(18), nil) // 1 ETH
	address := auth.From

	genesisAlloc := map[common.Address]types.Account{
		address: {Balance: balance},
	}

	// Create simulated blockchain
	// TODO: Fix this deprecation warning
	blockchain := backends.NewSimulatedBackend(genesisAlloc, 8000000)
	defer blockchain.Close()

	// Deploy contract
	contractAddress, tx, _ /* contract */, err := contract.DeployContract(auth, blockchain)
	if err != nil {
		t.Fatalf("Failed to deploy contract: %v", err)
	}
	blockchain.Commit()

	// Log deployment info
	t.Logf("Contract deployed at %s, tx: %s", contractAddress.Hex(), tx.Hash().Hex())

	/*
		// Test initial value
		number, err := contract.Number(&bind.CallOpts{})
		if err != nil {
			t.Fatalf("Failed to get number: %v", err)
		}
		if number.Cmp(big.NewInt(0)) != 0 {
			t.Fatalf("Initial number is not 0: %s", number.String())
		}

		// Test setNumber
		tx, err = contract.SetNumber(auth, big.NewInt(42))
		if err != nil {
			t.Fatalf("Failed to set number: %v", err)
		}
		blockchain.Commit()

		number, _ = contract.Number(&bind.CallOpts{})
		if number.Cmp(big.NewInt(42)) != 0 {
			t.Fatalf("Number not set correctly, got: %s, want: 42", number.String())
		}

		// Test increment
		tx, err = contract.Increment(auth)
		if err != nil {
			t.Fatalf("Failed to increment: %v", err)
		}
		blockchain.Commit()

		number, _ = contract.Number(&bind.CallOpts{})
		if number.Cmp(big.NewInt(43)) != 0 {
			t.Fatalf("Number not incremented correctly, got: %s, want: 43", number.String())
		}
	*/
}

const AnvilURL = "http://localhost:8545"
const AnvilPrivateKey0 = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func TestAnvil(t *testing.T) {
	// Connect to Anvil (start anvil first with: anvil)
	client, err := ethclient.Dial(AnvilURL)
	if err != nil {
		log.Fatalf("Failed to connect to Anvil: %v", err)
	}

	// Use a predefined Anvil test account
	privateKey, _ := crypto.HexToECDSA(AnvilPrivateKey0) // Anvil default account #0

	// Create auth
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, big.NewInt(31337)) // Anvil chain ID
	if err != nil {
		log.Fatalf("Failed to create auth: %v", err)
	}

	// Deploy contract
	contractAddress, tx, _ /* contract */, err := contract.DeployContract(auth, client)
	if err != nil {
		log.Fatalf("Failed to deploy contract: %v", err)
	}

	log.Printf("Contract deployed at %s, tx: %s\n", contractAddress.Hex(), tx.Hash().Hex())

	/*
		// Test initial value
		number, err := contract.Number(&bind.CallOpts{})
		if err != nil {
			log.Fatalf("Failed to get number: %v", err)
		}
		log.Printf("Initial number: %s\n", number.String())

		// Test setNumber
		tx, err = contract.SetNumber(auth, big.NewInt(42))
		if err != nil {
			log.Fatalf("Failed to set number: %v", err)
		}
		log.Printf("Set number tx: %s\n", tx.Hash().Hex())

		// Wait for transaction to be mined
		receipt, err := bind.WaitMined(context.Background(), client, tx)
		if err != nil {
			log.Fatalf("Failed waiting for tx to be mined: %v", err)
		}
		if receipt.Status == 0 {
			log.Fatal("Transaction failed")
		}

		number, _ = contract.Number(&bind.CallOpts{})
		log.Printf("Number after setNumber: %s\n", number.String())

		// Test increment
		tx, err = contract.Increment(auth)
		if err != nil {
			log.Fatalf("Failed to increment: %v", err)
		}
		log.Printf("Increment tx: %s\n", tx.Hash().Hex())

		// Wait for transaction to be mined
		receipt, err = bind.WaitMined(context.Background(), client, tx)
		if err != nil {
			log.Fatalf("Failed waiting for tx to be mined: %v", err)
		}

		number, _ = contract.Number(&bind.CallOpts{})
		log.Printf("Number after increment: %s\n", number.String())
	*/
}
