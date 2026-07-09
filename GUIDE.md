# Master Guide: Prob-Adaptive Inf-TESLA++
**A Dynamic Time-Scaling Extension for Resilient Broadcast Authentication**

This document serves as the master algorithmic, theoretical, and implementation guide for the Prob-Adaptive Inf-TESLA++ protocol extension. It is designed to immediately bring an engineering agent up to speed on the system's architecture, its core vulnerabilities, and the required code modifications.

---

## 1. The Core Vulnerability: Fixed-Slot Protocol Failures

### 1.1 The Disclosure Pipeline and Hardware Bottlenecks
In Inf-TESLA++ (and specifically its Probabilistic mode), the sender buffers raw data during a time slot. At the slot boundary, it compresses this data into a **Bloom Filter ($b_i$)**, computes an **HMAC ($h_i$)**, and immediately broadcasts the HMAC. The actual authentication evidence (the Bloom filter) and the cryptographic Key ($K_i$) are bundled into a `DisclosurePayload` and placed into a local holding queue (`disclosureMessages` channel) to be broadcast after a mandatory delay of $d$ slots.

**The Issue:** Resource-Constrained Devices (RCDs) utilize slow radio peripherals and shared mediums (like Wi-Fi or LoRa). 
* **MAC-Layer Backoff:** If the radio encounters interference, it pauses transmission. 
* **Internal Interrupt Storms/GC Pauses:** CPU spikes or Garbage Collection can stall the consumer thread (`disclosureWorker`).
While the transmitter is blocked, the application keeps generating data. If the slot duration is fixed (e.g., 1000ms), the rigid timer blindly forces the creation of new Bloom filters and disclosure payloads every second. The queue rapidly fills and overflows.

### 1.2 The Catastrophic Impact of Queue Overflow
In standard TESLA, dropping a key isn't fatal because the key can be derived from future keys. However, in **Inf-TESLA++ Probabilistic mode**, the `DisclosurePayload` contains the **Bloom filter byte array**. If the queue overflows and this payload is dropped, the Bloom filter is lost forever. Without the Bloom filter, the receiver cannot perform membership queries, meaning authentication for that entire batch of data is permanently broken.

### 1.3 The "Latency vs. Survivability" Trade-off
Why not use a permanently large static slot (e.g., 8000ms) to prevent overflow?
1. **Unacceptable Latency:** A permanently large slot forces everyday data to suffer massive authentication delays (e.g., 16 seconds if $d=2$), breaking real-time IoT/VANET use cases.
2. **Receiver DoS Vulnerability:** Receivers must buffer all unverified packets until the key arrives. A statically large slot allows attackers to easily flood the channel and exhaust receiver RAM.

---

## 2. The Theoretical Solution: Adaptive Time-Scaling

To solve the Latency-Survivability trade-off, the sender must employ a closed-loop controller that dynamically adjusts the slot duration ($T_{cur}$) within immutable, smart-contract-anchored bounds ($[T_{min}, T_{max}]$). 

### 2.1 Cryptographic Amortization (How it saves the system)
By dynamically stretching the slot duration during hardware bottlenecks, the protocol absorbs the shock. Instead of firing 4 times during a 4-second radio jam (generating 4 payloads), the stretched slot holds the batch open. It generates exactly **1 Bloom filter, 1 HMAC, and 1 Disclosure Payload** for the entire 4-second period. This drastically reduces the frequency of cryptographic processing and transmission overhead, allowing the CPU and radio to drain the queue and catch up without dropping evidence.

### 2.2 The EIP-1559 Inspired Algorithmic Trigger
Instead of arbitrary doubling (`*2`) or halving (`/2`), the protocol uses a smooth, mathematically bounded control loop based on **Target Queue Utilization** (similar to Ethereum's EIP-1559 block elasticity).

**The Variables:**
* $Q_{len}$: Current number of pending `DisclosurePayload` items.
* $Q_{cap}$: Total capacity of the disclosure queue.
* $U_{cur}$: Current utilization ($Q_{len} / Q_{cap}$).
* $U_{target}$: The desired healthy queue state (e.g., 0.50 or 50%).
* $\alpha$: The adjustment scaling factor.

**The Update Rule (Executed at every slot boundary):**
```text
Δ = U_{cur} - U_{target}
T_{next} = T_{cur} * (1 + (α * Δ))
T_{next} = MAX(T_{min}, MIN(T_{next}, T_{max}))
```

* If utilization is > 50%, $T_{next}$ smoothly scales up towards $T_{max}$.
* If utilization is < 50%, $T_{next}$ smoothly scales down towards $T_{min}$.

---

## 3. Implementation Overview & Required Modifications

### 3.1 Sender-Side Changes (Go Codebase - `rcd.go`)

1. **State Modifications:** The sender must track $T_{cur}$ and replace the static `time.Ticker` (or `slot.Ticker`) with a dynamic `time.Timer` that re-arms with $T_{next}$ after every slot.
2. **The Controller:** Implement the EIP-1559 logic at the end of the `broadcastLoop` or inside the `AdaptiveSlotSource`. The controller must safely read the length of the `disclosureMessages` channel without blocking.
3. **Mode Agnostic:** This logic applies to *both* Deterministic and Probabilistic modes. In Deterministic mode, it delays the key rotation, reducing the rate of delayed key broadcasts.

### 3.2 Smart Contract Changes (Solidity - `InfTESLAPlusPlus.sol`)

To prevent Desynchronization and Timing-Manipulation DoS attacks, the boundaries of the adaptive scaling must be immutable and globally visible.

1. **`storeAdaptiveKey`:** Create a new function (or modify `storeKey`) to accept and store two new parameters: `T_min` and `T_max`.
2. **`getKey`:** Update the getter to return `(K_0, d, l, tau_st, T_min, T_max)`.

### 3.3 Receiver-Side Changes (Go Codebase)

The receiver *must not* track the sender's real-time $T_{cur}$, as an attacker could spoof timing data to force premature key acceptance.

1. **Timing Safety Check:** The receiver fetches $T_{max}$ from the smart contract.
2. **Verification Logic:** The receiver calculates its security wait time based *strictly* on the maximum possible theoretical delay: `Wait_Time = d * T_max + network_delay`. This guarantees the TESLA timing condition is preserved regardless of how the sender's slot fluctuates internally.
