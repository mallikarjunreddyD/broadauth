package bloom

import (
	"encoding/binary"
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

// Check tests if data is in the Bloom Filter
func (f *Filter) Check(data []byte) bool {
	h := fnv.New64a()
	h.Write(data)
	hash1 := h.Sum64()
	h.Write([]byte{1})
	hash2 := h.Sum64()

	for i := uint(0); i < f.k; i++ {
		idx := (hash1 + uint64(i)*hash2) % uint64(f.m)
		byteIdx := idx / 8
		bitIdx := idx % 8
		if f.bitset[byteIdx]&(1<<bitIdx) == 0 {
			return false
		}
	}
	return true
}

// Bytes returns the serialized bitset with metadata (m and k)
func (f *Filter) Bytes() []byte {
	buf := make([]byte, 8+len(f.bitset))
	binary.BigEndian.PutUint32(buf[0:4], uint32(f.m))
	binary.BigEndian.PutUint32(buf[4:8], uint32(f.k))
	copy(buf[8:], f.bitset)
	return buf
}

// FromBytes reconstructs a Bloom Filter from bytes
func FromBytes(data []byte) *Filter {
	if len(data) < 8 {
		return nil
	}
	m := uint(binary.BigEndian.Uint32(data[0:4]))
	k := uint(binary.BigEndian.Uint32(data[4:8]))
	bitset := make([]byte, len(data)-8)
	copy(bitset, data[8:])
	return &Filter{
		bitset: bitset,
		k:      k,
		m:      m,
	}
}