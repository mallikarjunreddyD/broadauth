package hashchain_test

import (
	"crypto/sha256"
	"slices"
	"testing"

	"github.com/virinci/broadauth/pkg/hashchain"
)

func TestNew(t *testing.T) {
	hasher := sha256.New()
	hashchain.NewLinear(hasher, []byte("test"), 10)
}

func TestHasher(t *testing.T) {
	hasher := sha256.New()
	chain := hashchain.NewLinear(hasher, []byte("test"), 10)
	if chain.Hasher() != hasher {
		t.Errorf("Hasher not equal")
	}
}

func justHashIt(data []byte) []byte {
	hasher := sha256.New()
	hasher.Reset()
	hasher.Write(data)
	return hasher.Sum(nil)
}

func TestNext(t *testing.T) {
	seed := []byte("test")
	n := 10
	chain := hashchain.NewLinear(sha256.New(), seed, n)

	hashes := make([][]byte, n+1)
	hashes[n] = justHashIt(seed)

	for i := n - 1; i >= 0; i-- {
		hashes[i] = justHashIt(hashes[i+1])
	}

	if !slices.Equal(chain.First(), hashes[0]) {
		t.Errorf("0-th hash not equal")
	}
	if !slices.Equal(chain.Last(), hashes[n]) {
		t.Errorf("n-th hash not equal")
	}

	if chain.Remaining() != n {
		t.Errorf("Remaining incorrect")
	}

	for i := 1; i <= n; i++ {
		if !slices.Equal(chain.Next(), hashes[i]) {
			t.Errorf("%d-th hash not equal", i)
		}
		if chain.Remaining() != n-i {
			t.Errorf("Remaining incorrect")
		}
	}

	if !slices.Equal(chain.Next(), []byte{}) {
		t.Errorf("Hash not empty after exhausted")
	}
	if !slices.Equal(chain.Next(), []byte{}) {
		t.Errorf("Hash not empty after exhausted")
	}
}
