package workspacelock

import (
	"context"
	"errors"
	"sync"
)

var (
	// ErrForkActive is returned when a workspace mutation (e.g. file save or commit)
	// is attempted while a fork is in progress.
	ErrForkActive = errors.New("sandbox is currently being forked; please retry")

	// ErrForkInProgress is returned when a fork is requested on a sandbox that
	// is already being forked.
	ErrForkInProgress = errors.New("fork already in progress for this sandbox")

	// ErrLockReleased is returned when an operation is attempted on a lock that
	// has already been released.
	ErrLockReleased = errors.New("workspace lock already released")
)

// Manager coordinates concurrent access to sandbox workspaces.
// Every sandbox ID maps to exactly one active synchronization entry.
// Entries are reference-counted and safely cleaned up from the registry
// when unused, preventing duplicate lock creation.
type Manager struct {
	mu      sync.Mutex
	entries map[string]*sandboxEntry
}

// NewManager creates a ready-to-use workspace lock manager.
func NewManager() *Manager {
	return &Manager{
		entries: make(map[string]*sandboxEntry),
	}
}

type sandboxEntry struct {
	id       string
	refCount int

	mu   sync.Mutex
	cond *sync.Cond

	forking       bool // true while a fork owns or is waiting for the workspace
	activeWriters int  // count of writers currently holding write access
	writing       bool // true while a writer is actively executing
}

// getEntry returns the unique entry for the sandbox, creating it if needed.
// It increments refCount while holding m.mu.
func (m *Manager) getEntry(id string) *sandboxEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[id]
	if !ok {
		entry = &sandboxEntry{
			id: id,
		}
		entry.cond = sync.NewCond(&entry.mu)
		m.entries[id] = entry
	}
	entry.refCount++
	return entry
}

// releaseEntry decrements refCount and removes the entry from the registry
// when refCount reaches 0. It guarantees refCount can never become negative.
func (m *Manager) releaseEntry(entry *sandboxEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.refCount > 0 {
		entry.refCount--
	}
	if entry.refCount <= 0 {
		delete(m.entries, entry.id)
	}
}

// ActiveEntries returns the count of currently registered entries (for testing).
func (m *Manager) ActiveEntries() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// RefCount returns the active reference count for a sandbox entry, or 0 if unmapped.
func (m *Manager) RefCount(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.entries[id]; ok {
		return entry.refCount
	}
	return 0
}

// WriteLock represents exclusive write access to a sandbox's workspace for a
// file save, git commit, or other mutation.
type WriteLock struct {
	mgr      *Manager
	entry    *sandboxEntry
	released bool
	mu       sync.Mutex
}

// Release releases the write lock and notifies any waiting writers or fork operations.
// It is idempotent and safe to call multiple times or via defer.
func (l *WriteLock) Release() {
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return
	}
	l.released = true
	l.mu.Unlock()

	l.entry.mu.Lock()
	l.entry.writing = false
	l.entry.activeWriters--
	l.entry.cond.Broadcast()
	l.entry.mu.Unlock()

	l.mgr.releaseEntry(l.entry)
}

// AcquireSave acquires write access for saving a file to the sandbox workspace.
// If a fork is active or in progress, it returns ErrForkActive (HTTP 409).
// If another writer is currently saving, it waits until the active writer completes,
// preventing partial or corrupted concurrent writes.
func (m *Manager) AcquireSave(ctx context.Context, id string) (*WriteLock, error) {
	return m.acquireWrite(ctx, id)
}

// AcquireCommit acquires write access for committing changes to git.
func (m *Manager) AcquireCommit(ctx context.Context, id string) (*WriteLock, error) {
	return m.acquireWrite(ctx, id)
}

func (m *Manager) acquireWrite(ctx context.Context, id string) (*WriteLock, error) {
	entry := m.getEntry(id)

	entry.mu.Lock()

	// If a fork is already active or in-progress, reject immediately with ErrForkActive.
	if entry.forking {
		entry.mu.Unlock()
		m.releaseEntry(entry)
		return nil, ErrForkActive
	}

	// Set up cancellation listener if context is cancellable
	if ctx != nil && ctx.Done() != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				entry.mu.Lock()
				entry.cond.Broadcast()
				entry.mu.Unlock()
			case <-done:
			}
		}()
	}

	// Wait while another writer is actively writing
	for entry.writing {
		if entry.forking {
			entry.mu.Unlock()
			m.releaseEntry(entry)
			return nil, ErrForkActive
		}
		if ctx != nil && ctx.Err() != nil {
			entry.mu.Unlock()
			m.releaseEntry(entry)
			return nil, ctx.Err()
		}
		entry.cond.Wait()
	}

	// Double-check fork state upon waking
	if entry.forking {
		entry.mu.Unlock()
		m.releaseEntry(entry)
		return nil, ErrForkActive
	}
	if ctx != nil && ctx.Err() != nil {
		entry.mu.Unlock()
		m.releaseEntry(entry)
		return nil, ctx.Err()
	}

	entry.writing = true
	entry.activeWriters++
	entry.mu.Unlock()

	return &WriteLock{
		mgr:   m,
		entry: entry,
	}, nil
}

// ForkLock represents exclusive access to a parent sandbox's workspace during a fork.
type ForkLock struct {
	mgr      *Manager
	entry    *sandboxEntry
	released bool
	mu       sync.Mutex
}

// BeginFork initiates a fork for the given sandbox ID.
// If a fork is already in progress, it immediately returns ErrForkInProgress.
// It sets forking = true so that all subsequent saves or commits are rejected with ErrForkActive.
func (m *Manager) BeginFork(id string) (*ForkLock, error) {
	entry := m.getEntry(id)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.forking {
		m.releaseEntry(entry)
		return nil, ErrForkInProgress
	}

	entry.forking = true

	return &ForkLock{
		mgr:   m,
		entry: entry,
	}, nil
}

// WaitExclusive waits for any in-flight writers (e.g. saves that started before BeginFork)
// to complete. Once WaitExclusive returns nil, no writers are active, and no new writers
// can enter because forking == true.
func (l *ForkLock) WaitExclusive(ctx context.Context) error {
	l.entry.mu.Lock()
	defer l.entry.mu.Unlock()

	if ctx != nil && ctx.Done() != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				l.entry.mu.Lock()
				l.entry.cond.Broadcast()
				l.entry.mu.Unlock()
			case <-done:
			}
		}()
	}

	for l.entry.activeWriters > 0 {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		l.entry.cond.Wait()
	}

	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	return nil
}

// Release releases the fork lock and allows future saves and forks on the sandbox.
// It is idempotent and panic-safe.
func (l *ForkLock) Release() {
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return
	}
	l.released = true
	l.mu.Unlock()

	l.entry.mu.Lock()
	l.entry.forking = false
	l.entry.cond.Broadcast()
	l.entry.mu.Unlock()

	l.mgr.releaseEntry(l.entry)
}
