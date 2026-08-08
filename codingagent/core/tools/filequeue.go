package tools

import (
	"path/filepath"
	"sync"
)

// fileMutationQueue serializes file mutation operations targeting the same
// file. Operations for different files still run concurrently.
//
// Deviation from TS: the TS implementation chains promises per resolved
// realpath key to serialize access. Go has real mutexes, so this is
// implemented directly with a map of per-file mutexes guarded by a package
// mutex, which is simpler and has the same observable behavior.
type fileMutationQueue struct {
	mu     sync.Mutex
	queues map[string]*sync.Mutex
}

var globalFileMutationQueue = &fileMutationQueue{
	queues: make(map[string]*sync.Mutex),
}

func mutationQueueKey(filePath string) string {
	resolved, err := filepath.Abs(filePath)
	if err != nil {
		return filePath
	}
	if real, err := filepath.EvalSymlinks(resolved); err == nil {
		return real
	}
	return resolved
}

func (q *fileMutationQueue) lockFor(key string) *sync.Mutex {
	q.mu.Lock()
	defer q.mu.Unlock()
	m, ok := q.queues[key]
	if !ok {
		m = &sync.Mutex{}
		q.queues[key] = m
	}
	return m
}

// WithFileMutationQueue serializes fn against other mutation calls targeting
// the same resolved file path.
func WithFileMutationQueue[T any](filePath string, fn func() (T, error)) (T, error) {
	key := mutationQueueKey(filePath)
	m := globalFileMutationQueue.lockFor(key)
	m.Lock()
	defer m.Unlock()
	return fn()
}
