package mirror

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FilesystemCAS persists content-addressed objects under:
//
//   <root>/sha256/<first-two-hex>/<full-digest>
//
// Writes are staged in the destination directory and atomically renamed into
// place. Every successful Put performs a read-after-write digest verification.
type FilesystemCAS struct {
	root string
}

func NewFilesystemCAS(root string) (*FilesystemCAS, error) {
	if root == "" {
		return nil, errors.New("filesystem CAS root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve filesystem CAS root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absolute, "sha256"), 0o755); err != nil {
		return nil, fmt.Errorf("create filesystem CAS root: %w", err)
	}
	return &FilesystemCAS{root: absolute}, nil
}

func (s *FilesystemCAS) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *FilesystemCAS) Put(content []byte) (string, error) {
	if s == nil || s.root == "" {
		return "", errors.New("filesystem CAS is not initialized")
	}

	digest := rawSHA256(content)
	ref := casRef(digest)
	path := s.objectPath(digest)

	if _, err := os.Stat(path); err == nil {
		resolved, resolveErr := s.Resolve(ref)
		if resolveErr != nil {
			return "", fmt.Errorf("existing CAS object failed verification: %w", resolveErr)
		}
		if rawSHA256(resolved) != digest {
			return "", errors.New("existing CAS object does not match requested digest")
		}
		return ref, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat CAS object: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create CAS object directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".cas-write-*")
	if err != nil {
		return "", fmt.Errorf("create CAS temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("chmod CAS temp file: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write CAS temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("sync CAS temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close CAS temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("commit CAS object atomically: %w", err)
	}
	cleanup = false

	resolved, err := s.Resolve(ref)
	if err != nil {
		return "", fmt.Errorf("read-after-write CAS verification: %w", err)
	}
	if rawSHA256(resolved) != digest {
		return "", errors.New("read-after-write CAS digest mismatch")
	}
	return ref, nil
}

func (s *FilesystemCAS) Resolve(ref string) ([]byte, error) {
	if s == nil || s.root == "" {
		return nil, errors.New("filesystem CAS is not initialized")
	}
	digest, err := digestFromCASRef(ref)
	if err != nil {
		return nil, err
	}
	path := s.objectPath(digest)
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("CAS object %q not found", ref)
		}
		return nil, fmt.Errorf("read CAS object: %w", err)
	}
	actual := rawSHA256(content)
	if actual != digest {
		return nil, fmt.Errorf("CAS object digest mismatch: ref=%s actual=%s", digest, actual)
	}
	return content, nil
}

func (s *FilesystemCAS) objectPath(digest string) string {
	return filepath.Join(s.root, "sha256", digest[:2], digest)
}

func rawSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
