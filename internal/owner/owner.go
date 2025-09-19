package owner

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	contracts "github.com/virinci/broadauth/internal/contract"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/virinci/broadauth/pkg/hashchain"
)

type rcdState struct {
	registered          bool
	currentIndex        uint64
	lastStartTime       uint64
	lastEndTime         uint64
	lastDisclosureDelay uint64
}

type Owner struct {
	privateKey      *ecdsa.PrivateKey
	address         []byte
	cmAddr          string
	rcds            sync.Map
	listener        net.Listener
	hashchainLen    int
	ethClient       *ethclient.Client
	contract        *contracts.Contract
	auth            *bind.TransactOpts
	disclosureDelay uint64
}

type Config struct {
	CMAddr          string
	TCPAddr         string
	HashchainLen    int
	EthURL          string
	ContractAddr    string
	PrivateKey      string
	DisclosureDelay uint64
}

func New(cfg Config) (*Owner, error) {
	privateKey, err := crypto.HexToECDSA(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %v", err)
	}

	listener, err := net.Listen("tcp", cfg.TCPAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to start listener: %v", err)
	}

	pubKey := privateKey.Public().(*ecdsa.PublicKey)
	address := crypto.PubkeyToAddress(*pubKey).Bytes()

	// Connect to Ethereum
	client, err := ethclient.Dial(cfg.EthURL)
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to connect to Ethereum node: %v", err)
	}

	// Get chain ID
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		client.Close()
		listener.Close()
		return nil, fmt.Errorf("failed to get chain ID: %v", err)
	}

	// Create transactor
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		client.Close()
		listener.Close()
		return nil, fmt.Errorf("failed to create transactor: %v", err)
	}

	// Initialize contract
	contractAddress := common.HexToAddress(cfg.ContractAddr)
	contract, err := contracts.NewContract(contractAddress, client)
	if err != nil {
		client.Close()
		listener.Close()
		return nil, fmt.Errorf("failed to instantiate contract: %v", err)
	}

	return &Owner{
		privateKey:      privateKey,
		address:         address,
		cmAddr:          cfg.CMAddr,
		listener:        listener,
		hashchainLen:    cfg.HashchainLen,
		ethClient:       client,
		contract:        contract,
		auth:            auth,
		disclosureDelay: cfg.DisclosureDelay,
	}, nil
}

func (o *Owner) NewRCD() uuid.UUID {
	id := uuid.New()
	o.rcds.Store(id, &rcdState{registered: false})
	return id
}

func (o *Owner) registerRCD(id uuid.UUID) error {
	conn, err := net.Dial("tcp", o.cmAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to CM: %v", err)
	}
	defer conn.Close()

	payload := make([]byte, 36)
	copy(payload[:20], o.address)
	copy(payload[20:], id[:])

	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("failed to send registration: %v", err)
	}

	var resp struct {
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	if resp.Status != "success" {
		return fmt.Errorf("registration failed: %s", resp.Error)
	}

	return nil
}

func (o *Owner) ListenAndServe() error {
	for {
		conn, err := o.listener.Accept()
		if err != nil {
			return fmt.Errorf("accept failed: %v", err)
		}
		go o.handleRCD(conn)
	}
}

func (o *Owner) handleRCD(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 48)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return
	}

	var id uuid.UUID
	copy(id[:], buf[:16])

	log.Printf("Received hashchain request from RCD %s\n", id)

	state, exists := o.rcds.Load(id)
	if !exists {
		log.Printf("Unknown RCD %s, ignoring request\n", id)
		return
	}

	currentRCDState := state.(*rcdState)
	if !currentRCDState.registered {
		log.Printf("Registering RCD %s with CM\n", id)
		if err := o.registerRCD(id); err != nil {
			log.Printf("Failed to register RCD %s: %v\n", id, err)
			return
		}
		currentRCDState = &rcdState{registered: true, currentIndex: 0, lastEndTime: 0}
		o.rcds.Store(id, currentRCDState)
		log.Printf("Successfully registered RCD %s\n", id)
	}

	// Parse block number
	blockNum := new(big.Int).SetBytes(buf[16:48])

	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return
	}

	chain := hashchain.NewLinear(sha256.New(), seed, o.hashchainLen)
	first := chain.First()

	log.Printf("Generated new hashchain (commitment:%s) for RCD %s with length %d\n", hex.EncodeToString(first), id, o.hashchainLen)

	// Store key in contract
	ctx := context.Background()
	o.auth.Context = ctx

	rcdBigInt := new(big.Int).SetBytes(id[:])
	nextIndex := new(big.Int).SetUint64(currentRCDState.currentIndex + 1)
	startTime := blockNum
	endTime := new(big.Int).Add(blockNum, big.NewInt(int64(o.hashchainLen-1)))
	disclosureDelay := new(big.Int).SetUint64(o.disclosureDelay)

	log.Printf("Storing key for RCD %s: index=%d startTime=%d endTime=%d delay=%d\n",
		id, nextIndex.Uint64(), startTime.Uint64(), endTime.Uint64(), disclosureDelay.Uint64())

	tx, err := o.contract.StoreKey(
		o.auth,
		rcdBigInt,
		nextIndex,
		string(first),
		startTime,
		endTime,
		disclosureDelay,
	)
	if err != nil {
		log.Printf("Failed to store key for RCD %s: %v\n", id, err)
		return
	}

	// Wait for transaction to be mined
	log.Printf("Waiting for StoreKey transaction to be mined for RCD %s...\n", id)
	_, err = bind.WaitMined(ctx, o.ethClient, tx)
	if err != nil {
		log.Printf("Failed waiting for StoreKey transaction for RCD %s: %v\n", id, err)
		return
	}
	log.Printf("Successfully stored key for RCD %s\n", id)

	// If currentIndex is not 0, call changeCurrentIndex after storing new key
	if currentRCDState.currentIndex != 0 {
		log.Printf("Calling changeCurrentIndex for RCD %s at index %d\n", id, currentRCDState.currentIndex)
		tx, err := o.contract.ChangeCurrentIndex(o.auth, rcdBigInt)
		if err != nil {
			log.Printf("Failed to change current index for RCD %s: %v\n", id, err)
			return
		}

		log.Printf("Waiting for ChangeCurrentIndex transaction to be mined for RCD %s...\n", id)
		if _, err = bind.WaitMined(ctx, o.ethClient, tx); err != nil {
			log.Printf("Failed waiting for ChangeCurrentIndex transaction for RCD %s: %v\n", id, err)
			return
		}
		log.Printf("Successfully changed current index for RCD %s\n", id)
	}

	// Update RCD state
	currentRCDState.currentIndex++
	currentRCDState.lastStartTime = blockNum.Uint64()
	currentRCDState.lastEndTime = endTime.Uint64()
	currentRCDState.lastDisclosureDelay = o.disclosureDelay
	o.rcds.Store(id, currentRCDState)

	// Send hashchain
	log.Printf("Sending hashchain to RCD %s\n", id)
	conn.Write(first)
	sent := 1
	for hash := chain.Next(); len(hash) > 0; hash = chain.Next() {
		conn.Write(hash)
		sent++
	}
	log.Printf("Sent %d hashes to RCD %s\n", sent, id)
}

func (o *Owner) Close() error {
	o.ethClient.Close()
	return o.listener.Close()
}
