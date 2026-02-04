package bloom

import (
	"hash/fnv"
	"math"
)

type Filter struct {
	bitset []byte
	k      uint // Number of hash functions
	m      uint // Size of bitset in bits
}

// New creates a Bloom Filter optimized for n items with false positive probability p
func New(n uint, p float64) *Filter {
	// m = -n*ln(p) / (ln(2)^2)
	m := uint(math.Ceil(-1 * float64(n) * math.Log(p) / math.Pow(math.Log(2), 2)))
	// k = (m/n) * ln(2)
	k := uint(math.Ceil((float64(m) / float64(n)) * math.Log(2)))

	return &Filter{
		bitset: make([]byte, (m+7)/8),
		k:      k,
		m:      m,
	}
}

// Add inserts data into the Bloom Filter
func (f *Filter) Add(data []byte) {
	h := fnv.New64a()
	h.Write(data)
	hash1 := h.Sum64()
	h.Write([]byte{1}) // Simple salt for second hash
	hash2 := h.Sum64()

	for i := uint(0); i < f.k; i++ {
		// Double hashing technique: h_i = h1 + i*h2
		idx := (hash1 + uint64(i)*hash2) % uint64(f.m)
		byteIdx := idx / 8
		bitIdx := idx % 8
		f.bitset[byteIdx] |= (1 << bitIdx)
	}
}

// Bytes returns the serialized bitset
func (f *Filter) Bytes() []byte {
	return f.bitset
}