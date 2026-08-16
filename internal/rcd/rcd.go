package rcd

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"

	"github.com/virinci/broadauth/internal/broadcast"
	contracts "github.com/virinci/broadauth/internal/contract"
	"github.com/virinci/broadauth/internal/message"
	"github.com/virinci/broadauth/internal/slot"
	"github.com/virinci/broadauth/pkg/bloom"
	"github.com/virinci/broadauth/pkg/hashchain"
)

type Mode int

const (
	ModeDeterministic Mode = iota
	ModeProbabilistic
)

// Adaptive slot-timing controller constants (EIP-1559-style multiplicative
// rule, see selectNextDuration).
const (
	targetUtilization = 0.5  // healthy disclosure queue = half full
	adjustmentFactor  = 0.25 // caps a single step at +/-12.5% when Delta=+/-0.5

	// adaptiveQueueCap is the Q_cap the controller normalizes the disclosure
	// backlog against (U_cur = backlog / adaptiveQueueCap). It is deliberately
	// small and O(disclosureDelay): the healthy in-flight backlog is ~d+1
	// disclosures (each held d slots before release), so with cap 8 a healthy
	// system sits at U~=0.4 (gently biased toward Tmin / low latency), and any
	// transmission stall that pushes the backlog past 4 crosses the 0.5 target
	// and drives Tcur up. The old signal used cap(disclosureMessages)=1024,
	// against which the real backlog (~2) is U~=0.002 -> the controller could
	// only ever ratchet down to Tmin. This is the miscalibration that made the
	// toggle inert.
	adaptiveQueueCap = 8
)

// networkDelaySlack is the delta_net term in the receiver's premature-
// disclosure wait bound (d*Tmin - delta_net): tolerance subtracted from the
// fastest-legitimate-pace disclosure delay to absorb ordinary jitter
// between the HMAC and key disclosure's independent network deliveries.
const networkDelaySlack = 250 * time.Millisecond

// Metrics holds atomic counters for benchmarking
type Metrics struct {
	BytesSent        uint64
	BytesReceived    uint64
	MessagesSent     uint64
	MessagesReceived uint64
	OverheadBytes    uint64 // Bytes used for HMACs, Keys, BloomFilters (non-payload)

	// Timing Metrics (Cumulative Nanoseconds)
	HMACDuration      int64
	HMACCount         int64
	VerifyDuration    int64
	VerifyCount       int64
	BroadcastDuration int64
	BroadcastCount    int64

	// Receiver-side verification outcomes (adaptive mode)
	MessagesVerified         int64
	MessagesDroppedPremature int64
}

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

	// adaptiveSource is non-nil iff adaptive is true; it's the same object as
	// slotSource, kept as a concrete type so the controller can call
	// SetDuration/GetDuration without widening the shared SlotSource
	// interface for every other slot source implementation.
	adaptive       bool
	tMin           uint64 // ms, immutable floor from the smart contract
	tMax           uint64 // ms, immutable ceiling from the smart contract
	messageRate    int    // packets/sec injected by the traffic generator
	adaptiveSource *slot.AdaptiveSlotSource
	startTime      time.Time

	disclosureMessages chan DisclosurePayload

	// disclosureBacklog is the true Q_len the adaptive controller reads:
	// disclosures produced (enqueued) but not yet transmitted, counting BOTH
	// the hand-off channel AND disclosureWorker's local pending slice.
	// len(disclosureMessages) alone is useless as a signal because the worker
	// drains the whole channel into that local slice every tick, so the
	// channel is ~empty at every sample. Accessed atomically.
	disclosureBacklog int64

	receivedHMACs sync.Map // [32]byte -> time.Time (local receipt time)
	commitmentKeys     sync.Map
	senderTiming       sync.Map // uuid.UUID -> timingInfo, adaptive mode only

	// Buffer for Probabilistic Mode Receiver: Map[Slot] -> List of Data Messages
	unverifiedMsgs sync.Map

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	cachedKey     [32]byte
	cachedKeySlot uint64

	mode               Mode
	messageBuffer      [][]byte
	bufferMutex        sync.Mutex
	chainMutex         sync.Mutex
	enableBenchmarking bool

	// Performance Metrics
	metrics Metrics
}

type DisclosurePayload struct {
	Message    []byte
	Key        [32]byte
	TargetSlot uint64
}

type Config struct {
	UUID               uuid.UUID
	OwnerAddr          string
	EthURL             string
	ContractAddr       string
	HashchainLen       int
	DisclosureDelay    uint64
	SimulationTime     time.Duration
	Mode               Mode
	EnableBenchmarking bool

	// Adaptive slot timing (Prob-Adaptive Inf-TESLA++). Adaptive is
	// backward-compatible and defaults off: when false, TMin/TMax/MessageRate
	// are ignored and the RCD behaves exactly like the fixed-slot baseline.
	Adaptive    bool
	TMin        uint64 // ms
	TMax        uint64 // ms
	MessageRate int    // packets/sec for the traffic generator

	// RadioBytesPerSec throttles the shared broadcast radio (data + HMAC +
	// key disclosure) to model a constrained RCD link. <=0 leaves the radio
	// unthrottled (default). A tight budget is what makes the disclosure
	// backlog — and therefore the adaptive controller — actually respond to
	// load, without needing Linux `tc` shaping.
	RadioBytesPerSec int
}

// timingInfo caches a sender's disclosure delay and Tmin, fetched once from
// the smart contract via getAdaptiveKey and reused for every subsequent
// wall-clock safety check on that sender's disclosures (see the premature-
// disclosure check in handleMessage).
type timingInfo struct {
	disclosureDelay uint64
	tMin            uint64
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

	bcastCfg := broadcast.DefaultUDPConfig()
	bcastCfg.RadioBytesPerSec = cfg.RadioBytesPerSec
	broadcaster, err := broadcast.NewUDPBroadcaster(bcastCfg)
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

	var slotSource slot.SlotSource
	var adaptiveSource *slot.AdaptiveSlotSource
	if cfg.Adaptive {
		adaptiveSource = slot.NewAdaptiveSlotSource(cfg.TMin)
		slotSource = adaptiveSource
	} else {
		beaconSource, err := slot.NewBeaconChainSlotSource(client, 3*time.Second)
		if err != nil {
			return nil, fmt.Errorf("failed to create slot source: %v", err)
		}
		slotSource = beaconSource
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
		adaptive:           cfg.Adaptive,
		tMin:               cfg.TMin,
		tMax:               cfg.TMax,
		messageRate:        cfg.MessageRate,
		adaptiveSource:     adaptiveSource,
		disclosureMessages: make(chan DisclosurePayload, 1024),
		messageBuffer:      make([][]byte, 0),
		mode:               cfg.Mode,
		enableBenchmarking: cfg.EnableBenchmarking,
	}, nil
}

func (r *RCD) Start() error {
	r.ctx, r.cancel = context.WithTimeout(context.Background(), r.simulationTime)
	r.startTime = time.Now()

	r.receiver.SetMessageHandler(r.handleMessage)
	if err := r.receiver.Start(r.ctx); err != nil {
		return fmt.Errorf("failed to start receiver: %v", err)
	}

	// Logging Configuration based on Mode
	if r.enableBenchmarking {
		// Disable standard logs during benchmarking to keep output clean and fast
		log.SetOutput(io.Discard)
		r.wg.Add(4)
		go r.benchmarkWorker()
	} else {
		// Enable standard logs for normal operation
		log.SetOutput(os.Stderr)
		r.wg.Add(3)
	}

	go r.broadcastLoop()
	go r.disclosureWorker()
	go r.cleanupWorker()

	modeStr := "DETERMINISTIC"
	if r.mode == ModeProbabilistic {
		modeStr = "PROBABILISTIC"
	}
	// This log will print in normal mode, and be discarded in bench mode
	log.Printf("Starting RCD in %s mode", modeStr)

	return nil
}

func (r *RCD) Stop() error {
	r.cancel()
	r.wg.Wait()
	r.ethClient.Close()
	return nil
}

// benchmarkWorker prints stats every 5 seconds (Runs independently of hot path)
// It writes to Stdout, bypassing the log discard.
func (r *RCD) benchmarkWorker() {
	defer r.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var prevBytesSent, prevBytesRecv, prevMsgSent uint64
	var prevHMACDur, prevHMACCount int64
	var prevVerifyDur, prevVerifyCount int64
	var prevBroadcastDur, prevBroadcastCount int64

	startTime := time.Now()

	fmt.Println("   Time |      Tx Rate      |      Rx Rate      |  Msg/s  | Mem(MB) | Total Overhead | Avg HMAC (µs) | Avg Verify (µs) | Avg Bcast (µs) | Verified | PrematureDrop")
	fmt.Println("---------------------------------------------------------------------------------------------------------------------------------------------------------------")

	for {
		select {
		case <-r.ctx.Done():
			return
		case t := <-ticker.C:
			// Snapshot atomic counters (Cheap)
			currBytesSent := atomic.LoadUint64(&r.metrics.BytesSent)
			currBytesRecv := atomic.LoadUint64(&r.metrics.BytesReceived)
			currMsgSent := atomic.LoadUint64(&r.metrics.MessagesSent)
			overhead := atomic.LoadUint64(&r.metrics.OverheadBytes)

			currHMACDur := atomic.LoadInt64(&r.metrics.HMACDuration)
			currHMACCount := atomic.LoadInt64(&r.metrics.HMACCount)
			currVerifyDur := atomic.LoadInt64(&r.metrics.VerifyDuration)
			currVerifyCount := atomic.LoadInt64(&r.metrics.VerifyCount)
			currBroadcastDur := atomic.LoadInt64(&r.metrics.BroadcastDuration)
			currBroadcastCount := atomic.LoadInt64(&r.metrics.BroadcastCount)

			// Calculate rates over the last 5 seconds
			txRate := float64(currBytesSent-prevBytesSent) / 5.0
			rxRate := float64(currBytesRecv-prevBytesRecv) / 5.0
			msgRate := float64(currMsgSent-prevMsgSent) / 5.0

			// Calculate Average Latencies (Microseconds)
			avgHMAC := 0.0
			if diff := currHMACCount - prevHMACCount; diff > 0 {
				avgHMAC = float64(currHMACDur-prevHMACDur) / float64(diff) / 1000.0
			}

			avgVerify := 0.0
			if diff := currVerifyCount - prevVerifyCount; diff > 0 {
				avgVerify = float64(currVerifyDur-prevVerifyDur) / float64(diff) / 1000.0
			}

			avgBroadcast := 0.0
			if diff := currBroadcastCount - prevBroadcastCount; diff > 0 {
				avgBroadcast = float64(currBroadcastDur-prevBroadcastDur) / float64(diff) / 1000.0
			}

			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			memUsage := float64(m.Alloc) / 1024 / 1024

			elapsed := t.Sub(startTime).Seconds()

			verified := atomic.LoadInt64(&r.metrics.MessagesVerified)
			droppedPremature := atomic.LoadInt64(&r.metrics.MessagesDroppedPremature)

			// Print to Stdout (visible even if log is discarded)
			fmt.Printf("%7.1fs | %10.2f B/s | %10.2f B/s | %7.1f | %7.2f | %12d B | %12.2f | %14.2f | %13.2f | %8d | %13d\n",
				elapsed, txRate, rxRate, msgRate, memUsage, overhead, avgHMAC, avgVerify, avgBroadcast, verified, droppedPremature)

			prevBytesSent = currBytesSent
			prevBytesRecv = currBytesRecv
			prevMsgSent = currMsgSent
			prevHMACDur = currHMACDur
			prevHMACCount = currHMACCount
			prevVerifyDur = currVerifyDur
			prevVerifyCount = currVerifyCount
			prevBroadcastDur = currBroadcastDur
			prevBroadcastCount = currBroadcastCount
		}
	}
}

func (r *RCD) RequestHashChain(currentSlot uint64) error {
	const maxRetries = 3
	var lastErr error
	for i := range maxRetries {
		if err := r.requestHashChainOnce(currentSlot); err != nil {
			lastErr = err
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to request hashchain after retries: %v", lastErr)
}

func (r *RCD) requestHashChainOnce(currentSlot uint64) error {
	conn, err := net.Dial("tcp", r.ownerAddr)
	if err != nil {
		return fmt.Errorf("failed to dial owner: %v", err)
	}
	defer conn.Close()

	payload := make([]byte, 48)
	copy(payload[:16], r.id[:])
	new(big.Int).SetUint64(currentSlot).FillBytes(payload[16:48])

	if err := conn.SetDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return fmt.Errorf("failed to set deadline: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("failed to write request: %v", err)
	}

	chainSize := (r.hashchainLen + 1) * 32
	chain := make([]byte, chainSize)
	if _, err := io.ReadFull(conn, chain); err != nil {
		return fmt.Errorf("failed to read hashchain response: %v", err)
	}

	hashChain, err := hashchain.NewLinearFromExisting(sha256.New(), chain)
	if err != nil || hashChain.Remaining() < 1 {
		return fmt.Errorf("failed to initialize hashchain from bytes: %v", err)
	}

	r.hashChain = hashChain
	return nil
}

func (r *RCD) broadcastLoop() {
	defer r.wg.Done()
	// Traffic generator: rate is configurable via Config.MessageRate so
	// congestion-rate experiments (keychain lifespan vs. injection rate) are
	// actually meaningful. Defaults to 1/sec if unset/invalid.
	rate := r.messageRate
	if rate <= 0 {
		rate = 1
	}
	trafficTicker := time.NewTicker(time.Second / time.Duration(rate))
	defer trafficTicker.Stop()
	slotTicker := r.slotSource.Ticker(r.ctx)

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-trafficTicker.C:
			msg := []byte(fmt.Sprintf("%s: payload_data_%d", r.id, r.messageCounter))
			r.messageCounter++
			// Backgrounded (like the flushBatch call below): broadcast()
			// can block for seconds inside CurrentSlotKey() on a hashchain
			// refill (owner round-trip + on-chain confirmation). Running it
			// inline here would stall this select loop - and therefore
			// r.ctx.Done() responsiveness and Stop()'s wg.Wait() - for the
			// full duration of that refill.
			go func(m []byte) {
				if err := r.broadcast(m); err != nil {
					// Logs will be discarded if benchmarking is enabled
					log.Printf("Failed to broadcast message: %v", err)
				}
			}(msg)
		case currentSlot := <-slotTicker:
			go func(slotToFlush uint64) {
				if err := r.flushBatch(slotToFlush); err != nil {
					log.Printf("Failed to flush batch for slot %d: %v", slotToFlush, err)
				}
			}(currentSlot - 1)

			// Mode-agnostic: this fires every slot boundary regardless of
			// Mode, since flushBatch above already runs unconditionally
			// (it's a no-op on an empty buffer in deterministic mode).
			if r.adaptive {
				r.adjustSlotDuration()
			}
		}
	}
}

// selectNextDuration implements the EIP-1559-style multiplicative control
// rule from GUIDE.md: nudge the slot duration toward Tmax when the
// disclosure queue is more than targetUtilization full, and toward Tmin when
// it's less, clamped to [Tmin,Tmax].
//
// Qlen is the number of pending DisclosurePayload entries currently sitting
// in the disclosure queue (len(disclosureMessages)); Qcap is that queue's
// total capacity (cap(disclosureMessages)). One "disclosure event" is a
// single DisclosurePayload: the (key, evidence, targetSlot) bundle produced
// once per flush and held until the mandatory d-slot delay elapses.
func selectNextDuration(qlen, qcap int, tCur, tMin, tMax uint64) uint64 {
	if qcap <= 0 {
		return tCur
	}
	uCur := float64(qlen) / float64(qcap)
	delta := uCur - targetUtilization
	tNext := float64(tCur) * (1 + adjustmentFactor*delta)

	next := uint64(math.Round(tNext))
	if next < tMin {
		next = tMin
	}
	if next > tMax {
		next = tMax
	}
	return next
}

// adjustSlotDuration samples the current disclosure queue and re-arms the
// adaptive slot timer with the next duration. Logged via fmt.Printf (not
// log.Printf) so the line survives benchmarking mode, where log output is
// discarded but stdout is captured by the harness.
func (r *RCD) adjustSlotDuration() {
	// Q_len is the true disclosure backlog (channel + worker pending), not the
	// hand-off channel occupancy, which is drained to ~0 every tick. Q_cap is
	// the small, O(disclosureDelay) capacity that makes U_cur reachable.
	qlen := int(atomic.LoadInt64(&r.disclosureBacklog))
	if qlen < 0 {
		qlen = 0
	}
	qcap := adaptiveQueueCap
	tCur := r.adaptiveSource.GetDuration()
	tNext := selectNextDuration(qlen, qcap, tCur, r.tMin, r.tMax)
	r.adaptiveSource.SetDuration(tNext)

	uCur := 0.0
	if qcap > 0 {
		uCur = float64(qlen) / float64(qcap)
	}
	r.bufferMutex.Lock()
	ingest := len(r.messageBuffer)
	r.bufferMutex.Unlock()
	fmt.Printf("[PROB-ADAPTIVE] t=%.3fs T_cur=%dms U_cur=%.2f Qlen=%d Qcap=%d T_next=%dms Ingest=%d ChanLen=%d\n",
		time.Since(r.startTime).Seconds(), tCur, uCur, qlen, qcap, tNext, ingest, len(r.disclosureMessages))
}

func (r *RCD) CurrentSlotKey() (slot.Slot, []byte, error) {
	r.chainMutex.Lock()
	defer r.chainMutex.Unlock()

	for {
		if r.hashChain == nil || r.cachedKeySlot == 0 {
			// r.hashChain == nil only at startup (first fill); cachedKeySlot
			// dropping to 0 after a chain was in use means it was walked off
			// the end, i.e. genuine exhaustion. fmt.Printf (not log.Printf)
			// so this survives benchmarking mode's log-discard.
			if r.hashChain != nil {
				fmt.Printf("[KEYCHAIN] exhausted after %.3fs, refilling\n", time.Since(r.startTime).Seconds())
			}
			log.Printf("Hashchain exhausted or missing, refilling...")
			hashchainBeginSlot, err := r.slotSource.GetSlot()
			if err != nil {
				return 0, nil, fmt.Errorf("failed to get current slot: %v", err)
			}
			if err := r.RequestHashChain(uint64(hashchainBeginSlot)); err != nil {
				return 0, nil, fmt.Errorf("failed to request new hashchain: %v", err)
			}
			key := r.hashChain.Next()
			copy(r.cachedKey[:], key)
			r.cachedKeySlot = hashchainBeginSlot
		}

		currentSlot, err := r.slotSource.GetSlot()
		if err != nil {
			return 0, nil, fmt.Errorf("failed to get current slot in loop: %v", err)
		}

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
	currentSlot, _, err := r.CurrentSlotKey()
	if err != nil {
		return fmt.Errorf("failed to get current slot/key: %v", err)
	}

	dataMsg := message.NewMessage(r.id, currentSlot, message.MessageKindData, data)
	msgBytes, err := dataMsg.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal data message: %v", err)
	}

	if r.enableBenchmarking {
		atomic.AddUint64(&r.metrics.BytesSent, uint64(len(msgBytes)))
		atomic.AddUint64(&r.metrics.MessagesSent, 1)
	}

	start := time.Now()
	// Application data goes on the unthrottled plane: it must not consume the
	// auth-channel budget, or a high msg-rate starves the disclosure pipeline
	// (flushBatch would block on the radio before it could enqueue a
	// disclosure, so the controller's backlog signal never moves).
	err = r.broadcaster.BroadcastUnthrottled(r.ctx, msgBytes)
	if r.enableBenchmarking {
		atomic.AddInt64(&r.metrics.BroadcastDuration, time.Since(start).Nanoseconds())
		atomic.AddInt64(&r.metrics.BroadcastCount, 1)
	}

	if err != nil {
		return fmt.Errorf("failed to broadcast data message: %v", err)
	}

	if r.mode == ModeDeterministic {
		return r.broadcastDeterministic(data)
	} else {
		return r.bufferForBatch(data)
	}
}

func (r *RCD) broadcastDeterministic(data []byte) error {
	currentSlot, key, err := r.CurrentSlotKey()
	if err != nil {
		return fmt.Errorf("failed to get current slot/key for deterministic auth: %v", err)
	}

	startHMAC := time.Now()
	signature := r.calculateHMAC(key, data)
	if r.enableBenchmarking {
		atomic.AddInt64(&r.metrics.HMACDuration, time.Since(startHMAC).Nanoseconds())
		atomic.AddInt64(&r.metrics.HMACCount, 1)
	}

	hmacMessage := message.NewMessage(r.id, currentSlot, message.MessageKindHMAC, signature)
	hmacData, err := hmacMessage.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal HMAC message: %v", err)
	}

	if r.enableBenchmarking {
		l := uint64(len(hmacData))
		atomic.AddUint64(&r.metrics.BytesSent, l)
		atomic.AddUint64(&r.metrics.OverheadBytes, l)
	}

	startBroadcast := time.Now()
	err = r.broadcaster.Broadcast(r.ctx, hmacData)
	if r.enableBenchmarking {
		atomic.AddInt64(&r.metrics.BroadcastDuration, time.Since(startBroadcast).Nanoseconds())
		atomic.AddInt64(&r.metrics.BroadcastCount, 1)
	}

	if err != nil {
		return fmt.Errorf("failed to broadcast HMAC message: %v", err)
	}

	log.Printf("Sent HMAC message for slot %d", currentSlot)

	// +1: CurrentSlotKey() only advances once per tick, so many messages
	// generated throughout a single slot's real-time window all share this
	// same currentSlot/targetSlot - but disclosureWorker discloses all of
	// them simultaneously, at the single real tick where currentSlot
	// reaches targetSlot. A message HMAC'd right at the start of its slot
	// gets close to a full disclosureDelay ticks of real separation before
	// that moment; one HMAC'd right at the end of the same slot gets up to
	// one full tick less - the exact same real-time-vs-label mismatch as
	// flushBatch's, just arising from per-message variance within a shared
	// slot rather than a batch label offset. Anchoring one tick later
	// guarantees even the latest-in-slot message still gets the full
	// disclosureDelay ticks of real separation.
	targetSlot := currentSlot + r.disclosureDelay + 1
	keyArray := [32]byte{}
	copy(keyArray[:], key)

	select {
	case r.disclosureMessages <- DisclosurePayload{
		Message:    data,
		Key:        keyArray,
		TargetSlot: targetSlot,
	}:
		atomic.AddInt64(&r.disclosureBacklog, 1)
	default:
		// Queue full, drop disclosure to avoid blocking
		return fmt.Errorf("disclosure queue full")
	}
	return nil
}

func (r *RCD) bufferForBatch(data []byte) error {
	r.bufferMutex.Lock()
	defer r.bufferMutex.Unlock()
	r.messageBuffer = append(r.messageBuffer, data)
	return nil
}

func (r *RCD) flushBatch(slot uint64) error {
	r.bufferMutex.Lock()
	if len(r.messageBuffer) == 0 {
		r.bufferMutex.Unlock()
		return nil
	}
	messages := r.messageBuffer
	count := len(messages)
	r.messageBuffer = make([][]byte, 0)
	r.bufferMutex.Unlock()

	bf := bloom.New(uint(len(messages)), 0.01)
	for _, msg := range messages {
		bf.Add(msg)
	}
	bfData := bf.Bytes()

	_, key, err := r.CurrentSlotKey()
	if err != nil {
		return fmt.Errorf("failed to get current slot/key for batch flush: %v", err)
	}

	startHMAC := time.Now()
	signature := r.calculateHMAC(key, bfData)
	if r.enableBenchmarking {
		atomic.AddInt64(&r.metrics.HMACDuration, time.Since(startHMAC).Nanoseconds())
		atomic.AddInt64(&r.metrics.HMACCount, 1)
	}

	hmacMsg := message.NewMessage(r.id, slot, message.MessageKindHMAC, signature)
	hmacBytes, err := hmacMsg.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal batch HMAC message: %v", err)
	}

	if r.enableBenchmarking {
		l := uint64(len(hmacBytes))
		atomic.AddUint64(&r.metrics.BytesSent, l)
		atomic.AddUint64(&r.metrics.OverheadBytes, l)
	}

	startBroadcast := time.Now()
	err = r.broadcaster.Broadcast(r.ctx, hmacBytes)
	if r.enableBenchmarking {
		atomic.AddInt64(&r.metrics.BroadcastDuration, time.Since(startBroadcast).Nanoseconds())
		atomic.AddInt64(&r.metrics.BroadcastCount, 1)
	}

	if err != nil {
		return fmt.Errorf("failed to broadcast batch HMAC message: %v", err)
	}

	log.Printf("Sent HMAC message (Batch of %d messages) for slot %d", count, slot)

	// +1: this HMAC batch is labeled "slot" (the just-completed slot, see
	// broadcastLoop's flushBatch(currentSlot-1) call), but the broadcast
	// itself happens one tick later, at real time currentSlot. Computing
	// targetSlot from the label alone shorts the real-world gap between
	// this broadcast and disclosure eligibility by one full tick -
	// disclosureDelay=2 was only ever getting 1 real tick of separation.
	// Anchoring to the label+1 (the tick this actually went out on)
	// restores the intended disclosureDelay ticks of real elapsed time.
	targetSlot := slot + r.disclosureDelay + 1
	keyArray := [32]byte{}
	copy(keyArray[:], key)

	select {
	case r.disclosureMessages <- DisclosurePayload{
		Message:    bfData,
		Key:        keyArray,
		TargetSlot: targetSlot,
	}:
		atomic.AddInt64(&r.disclosureBacklog, 1)
	default:
		return fmt.Errorf("disclosure queue full")
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
			// Drain new disclosures from channel
			for {
				select {
				case disclosure := <-r.disclosureMessages:
					pendingDisclosures = append(pendingDisclosures, disclosure)
				default:
					goto ProcessPending
				}
			}
		ProcessPending:
			readyIdx := 0
			for i, disclosure := range pendingDisclosures {
				if disclosure.TargetSlot > currentSlot {
					readyIdx = i
					break
				}
				// This disclosure is leaving the backlog (transmitted or
				// dropped on a marshal error below — either way it is no
				// longer pending), so retire it from the gauge exactly once.
				atomic.AddInt64(&r.disclosureBacklog, -1)
				disclosureMsg := message.NewMessage(
					r.id,
					currentSlot,
					message.MessageKindKeyMessage,
					append(disclosure.Key[:], disclosure.Message...),
				)
				data, err := disclosureMsg.Marshal()
				if err == nil {
					if r.enableBenchmarking {
						l := uint64(len(data))
						atomic.AddUint64(&r.metrics.BytesSent, l)
						atomic.AddUint64(&r.metrics.OverheadBytes, l)
					}

					startBroadcast := time.Now()
					err := r.broadcaster.Broadcast(r.ctx, data)
					if r.enableBenchmarking {
						atomic.AddInt64(&r.metrics.BroadcastDuration, time.Since(startBroadcast).Nanoseconds())
						atomic.AddInt64(&r.metrics.BroadcastCount, 1)
					}

					if err != nil {
						log.Printf("Failed to broadcast disclosure: %v", err)
					} else {
						log.Printf("Sent key disclosure message for slot %d", currentSlot)
					}
				} else {
					log.Printf("Failed to marshal disclosure: %v", err)
				}
				readyIdx = i + 1
			}
			if readyIdx > 0 {
				pendingDisclosures = pendingDisclosures[readyIdx:]
			}
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *RCD) cleanupWorker() {
	defer r.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			currentSlot, err := r.slotSource.GetSlot()
			if err != nil {
				log.Printf("Failed to get slot for cleanup: %v", err)
				continue
			}
			retentionThreshold := r.disclosureDelay + 20
			r.unverifiedMsgs.Range(func(key, value interface{}) bool {
				if key.(uint64)+retentionThreshold < currentSlot {
					// Silent cleanup (unless debugging)
					r.unverifiedMsgs.Delete(key)
				}
				return true
			})
		}
	}
}

func (r *RCD) handleMessage(data []byte) {
	if r.enableBenchmarking {
		atomic.AddUint64(&r.metrics.BytesReceived, uint64(len(data)))
		atomic.AddUint64(&r.metrics.MessagesReceived, 1)
	}

	receivedMessage := &message.Message{}
	if err := receivedMessage.Unmarshal(data); err != nil {
		log.Printf("Error unmarshalling message: %v", err)
		return
	}

	currentSlot, err := r.slotSource.GetSlot()
	if err != nil {
		log.Printf("Failed to get current slot in handler: %v", err)
		return
	}

	if receivedMessage.Kind == message.MessageKindData {
		// This slot-count check is only meaningful when currentSlot and
		// receivedMessage.Slot are drawn from the same globally-synchronized
		// clock (the fixed-slot baseline). Under adaptive mode each process
		// free-runs its own AdaptiveSlotSource, so the two counters are not
		// comparable; the equivalent safety property is instead enforced
		// as a wall-clock check on key disclosure, below.
		if !r.adaptive {
			securityCutoff := receivedMessage.Slot + r.disclosureDelay
			if currentSlot >= securityCutoff {
				log.Printf("[SECURITY] Dropped message for slot %d (arrived too late)", receivedMessage.Slot)
				return
			}
		}
		r.storeUnverifiedMessage(receivedMessage.Slot, receivedMessage.Data)
		return
	}

	if receivedMessage.Kind == message.MessageKindKeyMessage {
		log.Printf("Received key disclosure message from %s for slot %d", receivedMessage.SenderID, receivedMessage.Slot)

		key, payload := receivedMessage.Data[:32], receivedMessage.Data[32:]

		var hmacArray [32]byte
		startHMAC := time.Now()
		copy(hmacArray[:], r.calculateHMAC(key, payload))
		if r.enableBenchmarking {
			atomic.AddInt64(&r.metrics.HMACDuration, time.Since(startHMAC).Nanoseconds())
			atomic.AddInt64(&r.metrics.HMACCount, 1)
		}

		hmacReceivedAtVal, exists := r.receivedHMACs.LoadAndDelete(hmacArray)
		if !exists {
			log.Printf("No matching HMAC found for key disclosure from %s", receivedMessage.SenderID)
			return
		}

		// Premature-disclosure check: don't trust a disclosed key until our
		// own local wall-clock stopwatch, started when we first saw this
		// slot's committed HMAC, shows at least d*Tmin - network_delay_slack
		// elapsed. Tmin is the fastest a protocol-compliant sender is ever
		// allowed to disclose, so nothing legitimate can arrive meaningfully
		// faster than d*Tmin - anything that does (beyond the slack below)
		// is either a sender violating its own on-chain-published floor or
		// a forged/replayed disclosure, and either way should be rejected.
		//
		// networkDelaySlack is SUBTRACTED, not added: hmacReceivedAt and
		// this elapsed measurement are both read from our own local clock,
		// so there's no cross-clock skew to compensate for here. What the
		// slack absorbs instead is jitter between the two independent
		// network deliveries (the HMAC and the key can each arrive slightly
		// faster or slower than the other) - which can make an honest,
		// exactly-Tmin-paced disclosure's *observed* elapsed time come out
		// smaller than the true underlying gap, never larger. Adding the
		// slack (the earlier version of this check) demanded more elapsed
		// time than even a perfectly-paced honest sender running at Tmin
		// could ever produce, rejecting 100% of calm-condition disclosures
		// - exactly the low-latency case GUIDE.md 1.3 says must work.
		// Subtracting it forgives that plausible negative jitter without
		// opening a meaningful attack window: a forger still has to land
		// inside a fixed, small band and still needs the real key, which
		// requires breaking the hash chain regardless of timing. Tmin comes
		// from the smart contract, never from the sender's live timing.
		if r.adaptive {
			timing, err := r.getSenderTiming(receivedMessage.SenderID)
			if err != nil {
				log.Printf("[SECURITY] Could not fetch timing policy for %s, rejecting disclosure: %v", receivedMessage.SenderID, err)
				atomic.AddInt64(&r.metrics.MessagesDroppedPremature, 1)
				return
			}
			minWait := time.Duration(timing.disclosureDelay*timing.tMin) * time.Millisecond
			if minWait > networkDelaySlack {
				minWait -= networkDelaySlack
			} else {
				minWait = 0
			}
			elapsed := time.Since(hmacReceivedAtVal.(time.Time))
			if elapsed < minWait {
				log.Printf("[SECURITY] Rejected premature key disclosure from %s (waited %s, need >= %s)", receivedMessage.SenderID, elapsed, minWait)
				atomic.AddInt64(&r.metrics.MessagesDroppedPremature, 1)
				return
			}
		}

		startVerify := time.Now()
		verified := r.verifyKey(receivedMessage.SenderID, key)
		if r.enableBenchmarking {
			atomic.AddInt64(&r.metrics.VerifyDuration, time.Since(startVerify).Nanoseconds())
			atomic.AddInt64(&r.metrics.VerifyCount, 1)
		}

		if !verified {
			log.Printf("Failed to verify key from %s", receivedMessage.SenderID)
			return
		}

		if r.mode == ModeProbabilistic {
			bf := bloom.FromBytes(payload)
			if bf != nil {
				targetSlot := receivedMessage.Slot - r.disclosureDelay
				msgs := r.getUnverifiedMessages(targetSlot)

				verifiedCount := 0
				for _, m := range msgs {
					if bf.Check(m) {
						log.Printf("[SUCCESS] Verified message via Bloom Filter: %s", string(m))
						verifiedCount++
					}
				}
				log.Printf("Probabilistic Batch Verification: %d messages authenticated for slot %d", verifiedCount, targetSlot)
				atomic.AddInt64(&r.metrics.MessagesVerified, int64(verifiedCount))
				r.unverifiedMsgs.Delete(targetSlot)
			}
		} else {
			// Deterministic mode: Payload IS the message
			log.Printf("[SUCCESS] Verified message from %s: %s", receivedMessage.SenderID, string(payload))
			atomic.AddInt64(&r.metrics.MessagesVerified, 1)
		}
	} else {
		// HMAC Message
		log.Printf("Received HMAC message from %s for slot %d", receivedMessage.SenderID, receivedMessage.Slot)
		var hmacArray [32]byte
		copy(hmacArray[:], receivedMessage.Data)
		r.receivedHMACs.Store(hmacArray, time.Now())
	}
}

func (r *RCD) storeUnverifiedMessage(slot uint64, data []byte) {
	value, _ := r.unverifiedMsgs.LoadOrStore(slot, make([][]byte, 0))
	msgs := value.([][]byte)
	msgs = append(msgs, data)
	r.unverifiedMsgs.Store(slot, msgs)
}

func (r *RCD) getUnverifiedMessages(slot uint64) [][]byte {
	value, ok := r.unverifiedMsgs.Load(slot)
	if !ok {
		return nil
	}
	return value.([][]byte)
}

// getSenderTiming lazily fetches and caches a sender's disclosure delay and
// Tmin from the smart contract via getAdaptiveKey. Also opportunistically
// caches the commitment key returned in the same call, so verifyKey's own
// lookup below normally finds it without a second contract round-trip.
func (r *RCD) getSenderTiming(senderID uuid.UUID) (timingInfo, error) {
	if v, ok := r.senderTiming.Load(senderID); ok {
		return v.(timingInfo), nil
	}

	contractKey, _, _, delay, tMin, _, err := r.contract.GetAdaptiveKey(&bind.CallOpts{}, new(big.Int).SetBytes(senderID[:]))
	if err != nil {
		return timingInfo{}, fmt.Errorf("failed to fetch adaptive key: %v", err)
	}

	info := timingInfo{disclosureDelay: delay.Uint64(), tMin: tMin.Uint64()}
	r.senderTiming.Store(senderID, info)

	if len(contractKey) > 0 {
		if _, exists := r.commitmentKeys.Load(senderID); !exists {
			r.commitmentKeys.Store(senderID, []byte(contractKey))
		}
	}

	return info, nil
}

func (r *RCD) verifyKey(senderID uuid.UUID, key []byte) bool {
	commitmentKey, ok := r.commitmentKeys.Load(senderID)
	if !ok {
		fetched, err := r.fetchCommitmentKey(senderID)
		if err != nil {
			return false
		}
		commitmentKey = fetched
		r.commitmentKeys.Store(senderID, commitmentKey)
	}

	if walksToCommitment(key, commitmentKey.([]byte), r.hashchainLen) {
		r.commitmentKeys.Store(senderID, key)
		return true
	}

	// The cached commitment can be stale: each keychain rotation (a
	// hashchain refill) starts a brand new, unrelated hash chain, so a
	// disclosed key from a freshly rotated keychain will never walk
	// forward to an old chain's anchor - commitmentKeys only ever ratchets
	// forward on success, so nothing else would refresh it here otherwise.
	// Re-fetch once from the contract (the source of truth for whichever
	// keychain is *currently* active for this sender) before giving up.
	fetched, err := r.fetchCommitmentKey(senderID)
	if err != nil {
		return false
	}
	if walksToCommitment(key, fetched, r.hashchainLen) {
		r.commitmentKeys.Store(senderID, key)
		return true
	}
	r.commitmentKeys.Store(senderID, fetched)
	return false
}

// fetchCommitmentKey reads a sender's current on-chain hash-chain anchor
// (adaptive or plain, matching this RCD's own mode).
func (r *RCD) fetchCommitmentKey(senderID uuid.UUID) ([]byte, error) {
	if r.adaptive {
		contractKey, _, _, _, _, _, err := r.contract.GetAdaptiveKey(&bind.CallOpts{}, new(big.Int).SetBytes(senderID[:]))
		if err != nil || len(contractKey) == 0 {
			return nil, fmt.Errorf("failed to fetch adaptive commitment key: %v", err)
		}
		return []byte(contractKey), nil
	}
	contractKey, _, _, _, err := r.contract.GetKey(&bind.CallOpts{}, new(big.Int).SetBytes(senderID[:]))
	if err != nil || len(contractKey) == 0 {
		return nil, fmt.Errorf("failed to fetch commitment key: %v", err)
	}
	return []byte(contractKey), nil
}

// walksToCommitment reports whether hashing key forward (up to
// hashchainLen+1 times) reaches commitment.
func walksToCommitment(key, commitment []byte, hashchainLen int) bool {
	currentKey := make([]byte, 32)
	copy(currentKey, key)

	if len(currentKey) == len(commitment) && hmac.Equal(currentKey, commitment) {
		return true
	}

	for range hashchainLen + 1 {
		h := sha256.New()
		h.Write(currentKey)
		currentKey = h.Sum(nil)
		if len(currentKey) == len(commitment) && hmac.Equal(currentKey, commitment) {
			return true
		}
	}
	return false
}

func (r *RCD) calculateHMAC(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
