package rcd

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"

	"github.com/virinci/broadauth/internal/broadcast"
	contracts "github.com/virinci/broadauth/internal/contract"
	"github.com/virinci/broadauth/internal/message"
	"github.com/virinci/broadauth/internal/slot"
	"github.com/virinci/broadauth/pkg/hashchain"
)

type RCD struct {
	id        uuid.UUID
	ownerAddr string
	ethClient *ethclient.Client
	contract  *contracts.Contract

	hashChain       *hashchain.Linear
	hashchainLen    int
	disclosureDelay uint64
	simulationTime  time.Duration
	messageCounter  uint64

	broadcaster broadcast.Broadcaster
	receiver    broadcast.Receiver
	slotSource  slot.SlotSource

	disclosureMessages chan DisclosurePayload
	receivedHMACs      sync.Map
	commitmentKeys     sync.Map // maps uuid.UUID to []byte

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	cachedKey     [32]byte
	cachedKeySlot uint64
}

type DisclosurePayload struct {
	Message    []byte
	Key        [32]byte
	TargetSlot uint64
}

type Config struct {
	UUID            uuid.UUID
	OwnerAddr       string
	EthURL          string
	ContractAddr    string
	HashchainLen    int
	DisclosureDelay uint64
	SimulationTime  time.Duration
}

func New(cfg Config) (*RCD, error) {
	client, err := ethclient.Dial(cfg.EthURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum node: %v", err)
	}

	contractAddress := common.HexToAddress(cfg.ContractAddr)
	contract, err := contracts.NewContract(contractAddress, client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to instantiate contract: %v", err)
	}

	broadcaster, err := broadcast.NewUDPBroadcaster(broadcast.DefaultUDPConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create broadcaster: %v", err)
	}

	receiver, err := broadcast.NewUDPReceiver(broadcast.DefaultUDPConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create receiver: %v", err)
	}

	if cfg.HashchainLen <= 0 || cfg.HashchainLen > 10*1024 {
		return nil, fmt.Errorf("invalid hashchain length: must be between 1 and %d", 10*1024)
	}

	slotSource, err := slot.NewBeaconChainSlotSource(client, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot source: %v", err)
	}

	return &RCD{
		id:                 cfg.UUID,
		ownerAddr:          cfg.OwnerAddr,
		ethClient:          client,
		contract:           contract,
		hashchainLen:       cfg.HashchainLen,
		disclosureDelay:    cfg.DisclosureDelay,
		simulationTime:     cfg.SimulationTime,
		broadcaster:        broadcaster,
		receiver:           receiver,
		slotSource:         slotSource,
		disclosureMessages: make(chan DisclosurePayload, 1024),
	}, nil
}

func (r *RCD) Start() error {
	// log.Printf("Starting RCD with ID %s", r.id)
	r.ctx, r.cancel = context.WithTimeout(context.Background(), r.simulationTime)

	r.receiver.SetMessageHandler(r.handleMessage)
	if err := r.receiver.Start(r.ctx); err != nil {
		return fmt.Errorf("failed to start receiver: %v", err)
	}

	r.wg.Add(2)
	go r.broadcastLoop()
	go r.disclosureWorker()

	// log.Printf("RCD started successfully")
	return nil
}

func (r *RCD) Stop() error {
	// log.Printf("Stopping RCD with ID %s", r.id)
	r.cancel()
	r.wg.Wait()
	r.ethClient.Close()
	// log.Printf("RCD stopped successfully")
	return nil
}

func (r *RCD) RequestHashChain(currentSlot uint64) error {
	const maxRetries = 3
	var lastErr error

	// log.Printf("Starting hashchain request for slot %d", currentSlot)
	for i := range maxRetries {
		if err := r.requestHashChainOnce(currentSlot); err != nil {
			lastErr = err
			// log.Printf("Attempt %d failed to request hashchain: %v", i+1, err)
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}
		// log.Printf("Successfully received hashchain for slot %d", currentSlot)
		return nil
	}
	return fmt.Errorf("failed to request hashchain after %d attempts: %v", maxRetries, lastErr)
}

func (r *RCD) requestHashChainOnce(currentSlot uint64) error {
	conn, err := net.Dial("tcp", r.ownerAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to owner: %v", err)
	}
	defer conn.Close()

	payload := make([]byte, 48)
	copy(payload[:16], r.id[:])
	new(big.Int).SetUint64(currentSlot).FillBytes(payload[16:48])

	if err := conn.SetDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return fmt.Errorf("failed to set connection deadline: %v", err)
	}

	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}

	chainSize := (r.hashchainLen + 1) * 32
	chain := make([]byte, chainSize)
	if _, err := io.ReadFull(conn, chain); err != nil {
		return fmt.Errorf("failed to read hashchain: %v", err)
	}

	hashChain, err := hashchain.NewLinearFromExisting(sha256.New(), chain)
	if err != nil || hashChain.Remaining() < 1 {
		return fmt.Errorf("failed to initialize hashchain: %v", err)
	}

	r.hashChain = hashChain
	return nil
}

func (r *RCD) broadcastLoop() {
	defer r.wg.Done()
	// log.Printf("Starting broadcast loop")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			// log.Printf("Broadcast loop stopping")
			return
		case <-ticker.C:
			msg := fmt.Sprintf("%s: mayday %d", r.id, r.messageCounter)
			r.messageCounter++
			if err := r.broadcast([]byte(msg)); err != nil {
				// log.Printf("Failed to broadcast: %v", err)
			}
		}
	}
}

// CurrentSlotKey returns the current slot and key for this slot.
// The slot is as close as possible to the slot when the function returns.
func (r *RCD) CurrentSlotKey() (slot.Slot, []byte, error) {
	for {
		if r.hashChain == nil || r.cachedKeySlot == 0 {
			log.Printf("No hashchain exists, requesting new one of length %d", r.hashchainLen)

			hashchainBeginSlot, err := r.slotSource.GetSlot()
			if err != nil {
				return 0, nil, fmt.Errorf("Failed to get current slot: %v", err)
			}

			if err := r.RequestHashChain(uint64(hashchainBeginSlot)); err != nil {
				return 0, nil, fmt.Errorf("Failed to request new hashchain: %v", err)
			}
			if r.hashChain.Remaining() != r.hashchainLen {
				log.Printf("Hashchain length mismatch, expected %d, got %d", r.hashchainLen, r.hashChain.Remaining())
				return 0, nil, fmt.Errorf("Hashchain length mismatch")
			}

			key := r.hashChain.Next()
			if len(key) == 0 {
				r.cachedKeySlot = 0
			} else {
				copy(r.cachedKey[:], key)
				r.cachedKeySlot = hashchainBeginSlot
			}
		}

		if r.cachedKeySlot == 0 {
			panic("Could not cache key even after requesting a new hashchain")
		}

		currentSlot, err := r.slotSource.GetSlot()
		if err != nil {
			return 0, nil, fmt.Errorf("Failed to get current slot: %v", err)
		}

		log.Printf("Current slot: %d, cached slot: %d", currentSlot, r.cachedKeySlot)

		for r.cachedKeySlot < currentSlot {
			if r.hashChain.Remaining() == 0 {
				r.cachedKeySlot = 0
				break
			}

			key := r.hashChain.Next()
			copy(r.cachedKey[:], key)
			r.cachedKeySlot++
		}

		if r.cachedKeySlot != 0 {
			return r.cachedKeySlot, r.cachedKey[:], nil
		}
	}
}

func (r *RCD) broadcast(data []byte) error {
	currentSlot, key, err := r.CurrentSlotKey()
	if err != nil {
		return fmt.Errorf("Failed to get current slot key: %v", err)
	}

	signature := r.calculateHMAC(key, data)
	hmacMessage := message.NewMessage(r.id, currentSlot, message.MessageKindHMAC, signature)

	hmacData, err := hmacMessage.Marshal()
	if err != nil {
		return err
	}

	if err := r.broadcaster.Broadcast(r.ctx, hmacData); err != nil {
		return err
	}
	log.Printf("Sent HMAC message for slot %d", currentSlot)

	targetSlot := currentSlot + r.disclosureDelay
	keyArray := [32]byte{}
	copy(keyArray[:], key)

	select {
	case r.disclosureMessages <- DisclosurePayload{
		Message:    data,
		Key:        keyArray,
		TargetSlot: targetSlot,
	}:
	default:
		return fmt.Errorf("disclosure message queue is full")
	}
	return nil
}

func (r *RCD) disclosureWorker() {
	defer r.wg.Done()
	ticker := r.slotSource.Ticker(r.ctx)
	pendingDisclosures := make([]DisclosurePayload, 0)

	for {
		select {
		case currentSlot := <-ticker:
			// Process any new disclosure messages
			for {
				select {
				case disclosure := <-r.disclosureMessages:
					pendingDisclosures = append(pendingDisclosures, disclosure)
				default:
					goto ProcessPending
				}
			}

		ProcessPending:
			// Check and broadcast ready disclosures
			readyIdx := 0
			for i, disclosure := range pendingDisclosures {
				if disclosure.TargetSlot > currentSlot {
					readyIdx = i
					break
				}

				// Create and broadcast disclosure message
				disclosureMsg := message.NewMessage(
					r.id,
					currentSlot,
					message.MessageKindKeyMessage,
					append(disclosure.Key[:], disclosure.Message...),
				)

				data, err := disclosureMsg.Marshal()
				if err != nil {
					log.Printf("Error marshalling disclosure message: %v", err)
					continue
				}

				if err := r.broadcaster.Broadcast(r.ctx, data); err != nil {
					log.Printf("Error broadcasting disclosure message: %v", err)
					continue
				}
				log.Printf("Sent key disclosure message for slot %d", currentSlot)
				readyIdx = i + 1
			}

			// Remove processed disclosures
			if readyIdx > 0 {
				pendingDisclosures = pendingDisclosures[readyIdx:]
			}

		case <-r.ctx.Done():
			return
		}
	}
}

func (r *RCD) handleMessage(data []byte) {
	receivedMessage := &message.Message{}
	if err := receivedMessage.Unmarshal(data); err != nil {
		log.Printf("Error unmarshalling message: %v", err)
		return
	}

	// currentSlot, err := r.slotSource.GetSlot()
	// if err != nil {
	// 	log.Printf("Failed to get current slot: %v", err)
	// 	return
	// }

	// TODO: Enable slot age validation after testing
	// if receivedMessage.Slot+r.disclosureDelay < currentSlot {
	// 	log.Printf("Discarding message from old slot %d (current: %d)", receivedMessage.Slot, currentSlot)
	// 	return
	// }

	if receivedMessage.Kind == message.MessageKindKeyMessage {
		key, payload := receivedMessage.Data[:32], receivedMessage.Data[32:]
		log.Printf("Received key disclosure message from %s for slot %d with key %s", receivedMessage.SenderID, receivedMessage.Slot, hex.EncodeToString(key))

		var hmacArray [32]byte
		copy(hmacArray[:], r.calculateHMAC(key, payload))

		if _, exists := r.receivedHMACs.LoadAndDelete(hmacArray); !exists {
			log.Printf("No matching HMAC found for key disclosure from %s", receivedMessage.SenderID)
			return
		}

		log.Printf("Verified HMAC for key disclosure from %s", receivedMessage.SenderID)

		log.Printf("Attempting to verify key from %s", receivedMessage.SenderID)
		verified := r.verifyKey(receivedMessage.SenderID, key)
		if verified {
			log.Printf("Successfully verified message from %s: %s", receivedMessage.SenderID, string(payload))
		} else {
			log.Printf("Failed to verify key from %s", receivedMessage.SenderID)
		}
	} else {
		log.Printf("Received HMAC message from %s for slot %d", receivedMessage.SenderID, receivedMessage.Slot)
		var hmacArray [32]byte
		copy(hmacArray[:], receivedMessage.Data)
		r.receivedHMACs.Store(hmacArray, true)
	}
}

func (r *RCD) verifyKey(senderID uuid.UUID, key []byte) bool {
	commitmentKey, ok := r.commitmentKeys.Load(senderID)
	if !ok {
		log.Printf("No commitment key stored, fetching from contract for %s", senderID)

		contractKey, startTime, endTime, delay, err := r.contract.GetKey(&bind.CallOpts{}, new(big.Int).SetBytes(senderID[:]))
		if err != nil {
			log.Printf("Failed to get key from contract: %v", err)
			return false
		}
		log.Printf("Got key from contract - startTime: %d, endTime: %d, delay: %d",
			startTime, endTime, delay)
		if len(contractKey) == 0 {
			log.Printf("Contract returned empty key")
			return false
		}
		commitmentKey = []byte(contractKey)
		r.commitmentKeys.Store(senderID, commitmentKey)
		log.Printf("Stored commitment key of length %d for %s", len(commitmentKey.([]byte)), senderID)
	}

	currentKey := make([]byte, 32)
	copy(currentKey, key)

	if len(currentKey) == len(commitmentKey.([]byte)) && hmac.Equal(currentKey, commitmentKey.([]byte)) {
		log.Printf("Key matches commitment directly (0 hashes)")
		r.commitmentKeys.Store(senderID, currentKey)
		return true
	}

	log.Printf("Starting hash chain verification")
	for i := range r.hashchainLen + 1 {
		h := sha256.New()
		h.Write(currentKey)
		currentKey = h.Sum(nil)

		if len(currentKey) == len(commitmentKey.([]byte)) && hmac.Equal(currentKey, commitmentKey.([]byte)) {
			log.Printf("Key verified after %d hashes", i+1)
			r.commitmentKeys.Store(senderID, key)
			return true
		}
	}
	log.Printf("Key verification failed after %d hashes", r.hashchainLen)

	return false
}

func (r *RCD) calculateHMAC(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
