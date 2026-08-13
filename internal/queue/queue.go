package queue

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/opspilot/opspilot-agent/internal/transport"
)

// Queue is an encrypted on-disk retry queue for events.
type Queue struct {
	mu      sync.Mutex
	dir     string
	key     []byte
	pending []transport.Event
}

// New creates a queue rooted at dataDir/queue.
func New(dataDir, keyMaterial string) (*Queue, error) {
	dir := filepath.Join(dataDir, "queue")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(keyMaterial))
	q := &Queue{dir: dir, key: sum[:]}
	if err := q.load(); err != nil {
		return nil, err
	}
	return q, nil
}

// Enqueue appends events and persists them encrypted.
func (q *Queue) Enqueue(events ...transport.Event) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, events...)
	return q.persistLocked()
}

// Peek returns a copy of pending events up to limit.
func (q *Queue) Peek(limit int) []transport.Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit <= 0 || limit > len(q.pending) {
		limit = len(q.pending)
	}
	out := make([]transport.Event, limit)
	copy(out, q.pending[:limit])
	return out
}

// Len returns the number of pending events.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// Drop removes the first n events after a successful send.
func (q *Queue) Drop(n int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if n <= 0 {
		return nil
	}
	if n > len(q.pending) {
		n = len(q.pending)
	}
	q.pending = append([]transport.Event{}, q.pending[n:]...)
	return q.persistLocked()
}

func (q *Queue) persistLocked() error {
	data, err := json.Marshal(q.pending)
	if err != nil {
		return err
	}
	sealed, err := q.encrypt(data)
	if err != nil {
		return err
	}
	path := filepath.Join(q.dir, "events.bin")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (q *Queue) load() error {
	path := filepath.Join(q.dir, "events.bin")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	plain, err := q.decrypt(data)
	if err != nil {
		// Corrupt or key mismatch — start fresh rather than crash.
		_ = os.Remove(path)
		return nil
	}
	var events []transport.Event
	if err := json.Unmarshal(plain, &events); err != nil {
		_ = os.Remove(path)
		return nil
	}
	q.pending = events
	return nil
}

func (q *Queue) encrypt(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(q.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func (q *Queue) decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(q.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// FlushFunc sends a batch of events.
type FlushFunc func(events []transport.Event) error

// RunFlusher periodically attempts to drain the queue.
func (q *Queue) RunFlusher(stop <-chan struct{}, interval time.Duration, max int, flush FlushFunc) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			batch := q.Peek(max)
			if len(batch) == 0 {
				continue
			}
			if err := flush(batch); err != nil {
				continue
			}
			_ = q.Drop(len(batch))
		}
	}
}
