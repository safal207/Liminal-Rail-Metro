package mirror

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// CASResolver resolves content by a transport-neutral cas://sha256/<digest>
// reference. Implementations must verify that returned bytes match the digest.
type CASResolver interface {
	Resolve(ref string) ([]byte, error)
}

// CASStore is the minimal writable extension used to persist evidence artifacts.
type CASStore interface {
	CASResolver
	Put(content []byte) (string, error)
}

// MemoryCAS is a deterministic in-memory CAS useful for tests and local flows.
// Remote/filesystem implementations can satisfy the same interface later.
type MemoryCAS struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

func NewMemoryCAS() *MemoryCAS {
	return &MemoryCAS{objects: make(map[string][]byte)}
}

func (s *MemoryCAS) Put(content []byte) (string, error) {
	if s == nil {
		return "", errors.New("nil memory CAS")
	}
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	ref := casRef(digest)

	copyBytes := append([]byte(nil), content...)
	s.mu.Lock()
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[digest] = copyBytes
	s.mu.Unlock()
	return ref, nil
}

func (s *MemoryCAS) Resolve(ref string) ([]byte, error) {
	if s == nil {
		return nil, errors.New("nil memory CAS")
	}
	digest, err := digestFromCASRef(ref)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	content, ok := s.objects[digest]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("CAS object %q not found", ref)
	}

	sum := sha256.Sum256(content)
	actual := hex.EncodeToString(sum[:])
	if actual != digest {
		return nil, fmt.Errorf("CAS object digest mismatch: ref=%s actual=%s", digest, actual)
	}
	return append([]byte(nil), content...), nil
}

func digestFromCASRef(ref string) (string, error) {
	const prefix = "cas://sha256/"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("unsupported CAS ref %q", ref)
	}
	digest := strings.TrimPrefix(ref, prefix)
	if !isSHA256Hex(digest) {
		return "", fmt.Errorf("invalid sha256 CAS digest %q", digest)
	}
	return digest, nil
}
