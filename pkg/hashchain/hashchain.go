package hashchain

import (
	"fmt"
	"hash"
)

type HashChain interface {
	// First returns the first hash (h_0) in the hash chain.
	First() []byte

	// Last returns the last hash (h_n) in the hash chain.
	Last() []byte

	// Next returns an empty slice if no hashes remain in the hash chain, else returns the next hash.
	// It would return hashes h_1, h_2, ..., h_n.
	// NOTE: h_0 is never returned. First() can be called to get it.
	Next() []byte

	// Remaining returns the number of hashes remaining in the hash chain.
	Remaining() int

	// Hasher returns the hasher that is being used for generating the hash chain.
	Hasher() hash.Hash
}

type Linear struct {
	hasher hash.Hash
	n      int
	chain  []byte
	next   int
}

func NewLinear(hasher hash.Hash, seed []byte, n int) *Linear {
	hasher.Reset()
	hasher.Write(seed)
	last := hasher.Sum(nil)

	hashLen := len(last)
	chain := make([]byte, (n+1)*hashLen)
	copy(chain[n*hashLen:], last)

	for i := n - 1; i >= 0; i-- {
		curr, next := i*hashLen, (i+1)*hashLen
		hasher.Reset()
		hasher.Write(chain[next : next+hashLen])
		copy(chain[curr:next], hasher.Sum(nil))
	}

	return &Linear{hasher, n, chain, 1}
}

func (s *Linear) Hasher() hash.Hash {
	return s.hasher
}

func (s *Linear) First() []byte {
	return s.chain[:s.hasher.Size()]
}

func (s *Linear) Last() []byte {
	return s.chain[s.n*s.hasher.Size():]
}

func (s *Linear) Next() []byte {
	if s.next > s.n {
		return []byte{}
	}

	next := s.chain[s.next*s.hasher.Size() : (s.next+1)*s.hasher.Size()]
	s.next += 1
	return next
}

func (s *Linear) Remaining() int {
	return s.n - s.next + 1
}

// verifyChain checks if each hash in the chain properly links to the next one
// h_i = hash(h_{i+1}) for i = 0 to n-1
func verifyChain(hasher hash.Hash, chain []byte) bool {
	hashLen := hasher.Size()
	n := (len(chain) / hashLen) - 1

	for i := range n {
		curr := chain[i*hashLen : (i+1)*hashLen]
		next := chain[(i+1)*hashLen : (i+2)*hashLen]

		hasher.Reset()
		hasher.Write(next)
		expected := hasher.Sum(nil)

		if string(curr) != string(expected) {
			return false
		}
	}
	return true
}

// NewLinearFromExisting creates a Linear hash chain from an existing chain of hashes.
// The chain should be a concatenation of all hashes in order (h_0 || h_1 || ... || h_n).
// The hasher must match the one used to create the original chain.
func NewLinearFromExisting(hasher hash.Hash, chain []byte) (*Linear, error) {
	hashLen := hasher.Size()
	if len(chain)%hashLen != 0 {
		return nil, fmt.Errorf("chain length must be multiple of hash size")
	}
	n := (len(chain) / hashLen) - 1 // -1 because n is the number of hashes after h_0

	if !verifyChain(hasher, chain) {
		return nil, fmt.Errorf("invalid hash chain")
	}

	return &Linear{hasher, n, chain, 1}, nil
}
