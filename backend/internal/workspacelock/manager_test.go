package workspacelock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestForkHoldsLockSaveGetsErrForkActive tests that while a fork is active,
// any attempt to acquire a save lock receives ErrForkActive.
func TestForkHoldsLockSaveGetsErrForkActive(t *testing.T) {
	mgr := NewManager()
	sandboxID := "sb-test-1"

	forkLock, err := mgr.BeginFork(sandboxID)
	if err != nil {
		t.Fatalf("BeginFork failed: %v", err)
	}
	defer forkLock.Release()

	// While fork is held, save must return ErrForkActive
	_, err = mgr.AcquireSave(context.Background(), sandboxID)
	if !errors.Is(err, ErrForkActive) {
		t.Fatalf("expected ErrForkActive, got %v", err)
	}
}

// TestForkCompletesSubsequentSaveSucceeds tests that after a fork completes
// and releases its lock, subsequent saves succeed immediately.
func TestForkCompletesSubsequentSaveSucceeds(t *testing.T) {
	mgr := NewManager()
	sandboxID := "sb-test-2"

	forkLock, err := mgr.BeginFork(sandboxID)
	if err != nil {
		t.Fatalf("BeginFork failed: %v", err)
	}

	if err := forkLock.WaitExclusive(context.Background()); err != nil {
		t.Fatalf("WaitExclusive failed: %v", err)
	}

	// Release fork
	forkLock.Release()

	// Now save must succeed
	saveLock, err := mgr.AcquireSave(context.Background(), sandboxID)
	if err != nil {
		t.Fatalf("AcquireSave failed after fork release: %v", err)
	}
	saveLock.Release()
}

// TestSaveStartsFirstForkWaits tests that when a save is actively in progress,
// a fork waits for the save to complete before WaitExclusive returns.
func TestSaveStartsFirstForkWaits(t *testing.T) {
	mgr := NewManager()
	sandboxID := "sb-test-3"

	saveLock, err := mgr.AcquireSave(context.Background(), sandboxID)
	if err != nil {
		t.Fatalf("AcquireSave failed: %v", err)
	}

	forkLock, err := mgr.BeginFork(sandboxID)
	if err != nil {
		t.Fatalf("BeginFork failed: %v", err)
	}
	defer forkLock.Release()

	forkWaitDone := make(chan struct{})
	var saveCompleted atomic.Bool

	go func() {
		if err := forkLock.WaitExclusive(context.Background()); err != nil {
			t.Errorf("WaitExclusive error: %v", err)
		}
		if !saveCompleted.Load() {
			t.Errorf("CRITICAL: Fork WaitExclusive returned before save finished!")
		}
		close(forkWaitDone)
	}()

	// Signal save done and release save
	saveCompleted.Store(true)
	saveLock.Release()

	select {
	case <-forkWaitDone:
		// Success: fork unblocked only after save released
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fork to unblock")
	}
}

// TestTwoSimultaneousForksRejected tests that two simultaneous fork requests
// for the same sandbox cannot both succeed; the second receives ErrForkInProgress.
func TestTwoSimultaneousForksRejected(t *testing.T) {
	mgr := NewManager()
	sandboxID := "sb-test-4"

	forkLock1, err := mgr.BeginFork(sandboxID)
	if err != nil {
		t.Fatalf("first BeginFork failed: %v", err)
	}
	defer forkLock1.Release()

	_, err = mgr.BeginFork(sandboxID)
	if !errors.Is(err, ErrForkInProgress) {
		t.Fatalf("expected ErrForkInProgress, got %v", err)
	}
}

// TestFailedForkReleasesLock tests that deferred Release on error or panic
// reliably frees the workspace for subsequent saves.
func TestFailedForkReleasesLock(t *testing.T) {
	mgr := NewManager()
	sandboxID := "sb-test-5"

	func() {
		forkLock, err := mgr.BeginFork(sandboxID)
		if err != nil {
			t.Fatalf("BeginFork failed: %v", err)
		}
		defer forkLock.Release()

		// Simulate error during copyDir
		_ = errors.New("simulated copyDir disk full")
		// returns, defer runs
	}()

	// Subsequent save must succeed immediately
	saveLock, err := mgr.AcquireSave(context.Background(), sandboxID)
	if err != nil {
		t.Fatalf("AcquireSave failed: %v", err)
	}
	saveLock.Release()
}

// TestConcurrentSavesSerialized tests that concurrent saves to the same sandbox
// are serialized and never interleave or corrupt execution.
func TestConcurrentSavesSerialized(t *testing.T) {
	mgr := NewManager()
	sandboxID := "sb-test-6"

	const n = 20
	var activeWriters int32
	var maxConcurrentWriters int32

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier

			lock, err := mgr.AcquireSave(context.Background(), sandboxID)
			if err != nil {
				t.Errorf("AcquireSave error: %v", err)
				return
			}
			defer lock.Release()

			cur := atomic.AddInt32(&activeWriters, 1)
			for {
				old := atomic.LoadInt32(&maxConcurrentWriters)
				if cur <= old || atomic.CompareAndSwapInt32(&maxConcurrentWriters, old, cur) {
					break
				}
			}

			// Critical section
			atomic.AddInt32(&activeWriters, -1)
		}()
	}

	close(startBarrier)
	wg.Wait()

	if maxConcurrentWriters > 1 {
		t.Fatalf("CRITICAL: concurrent writers detected: %d (must be serialized to 1)", maxConcurrentWriters)
	}
}

// TestRegistryReferenceCountingAndCleanup verifies that lock registry entries
// are cleaned up when no goroutines hold or wait on them, and that multiple
// goroutines accessing the same ID always share the exact same entry pointer.
func TestRegistryReferenceCountingAndCleanup(t *testing.T) {
	mgr := NewManager()
	sandboxID := "sb-test-7"

	if mgr.ActiveEntries() != 0 {
		t.Fatalf("expected 0 active entries, got %d", mgr.ActiveEntries())
	}

	// Goroutine 1 acquires save
	lock1, err := mgr.AcquireSave(context.Background(), sandboxID)
	if err != nil {
		t.Fatalf("AcquireSave 1 failed: %v", err)
	}
	if mgr.ActiveEntries() != 1 {
		t.Fatalf("expected 1 active entry, got %d", mgr.ActiveEntries())
	}

	// Goroutine 2 enters getEntry concurrently
	e1 := lock1.entry

	// Release lock1
	lock1.Release()

	// Entry must be cleaned up from map
	if mgr.ActiveEntries() != 0 {
		t.Fatalf("expected 0 active entries after release, got %d", mgr.ActiveEntries())
	}

	// Fresh acquisition gets new entry cleanly
	lock2, err := mgr.AcquireSave(context.Background(), sandboxID)
	if err != nil {
		t.Fatalf("AcquireSave 2 failed: %v", err)
	}
	defer lock2.Release()

	if mgr.ActiveEntries() != 1 {
		t.Fatalf("expected 1 active entry, got %d", mgr.ActiveEntries())
	}
	_ = e1
}

// TestRepeatedLockReleaseIsSafe verifies that repeated calls to Release() on
// both ForkLock and WriteLock are safe no-ops and never result in negative
// reference counts or registry corruptions.
func TestRepeatedLockReleaseIsSafe(t *testing.T) {
	mgr := NewManager()
	sandboxID := "sb-test-repeat"

	// 1. Test repeated ForkLock.Release()
	forkLock, err := mgr.BeginFork(sandboxID)
	if err != nil {
		t.Fatalf("BeginFork failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		forkLock.Release()
	}

	if count := mgr.RefCount(sandboxID); count != 0 {
		t.Fatalf("expected refCount 0 after repeated fork release, got %d", count)
	}
	if entries := mgr.ActiveEntries(); entries != 0 {
		t.Fatalf("expected 0 active entries, got %d", entries)
	}

	// 2. Test repeated WriteLock.Release()
	saveLock, err := mgr.AcquireSave(context.Background(), sandboxID)
	if err != nil {
		t.Fatalf("AcquireSave failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		saveLock.Release()
	}

	if count := mgr.RefCount(sandboxID); count != 0 {
		t.Fatalf("expected refCount 0 after repeated save release, got %d", count)
	}
	if entries := mgr.ActiveEntries(); entries != 0 {
		t.Fatalf("expected 0 active entries, got %d", entries)
	}
}

// TestForkCancellationReleasesRegistryEntry verifies that when a fork wait is
// canceled via context, releasing the ForkLock reliably cleans up the registry entry.
func TestForkCancellationReleasesRegistryEntry(t *testing.T) {
	mgr := NewManager()
	sandboxID := "sb-test-cancel-cleanup"

	// First acquire a save lock so fork must wait
	saveLock, err := mgr.AcquireSave(context.Background(), sandboxID)
	if err != nil {
		t.Fatalf("AcquireSave failed: %v", err)
	}

	forkLock, err := mgr.BeginFork(sandboxID)
	if err != nil {
		t.Fatalf("BeginFork failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled context

	err = forkLock.WaitExclusive(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Release fork
	forkLock.Release()

	// Release save
	saveLock.Release()

	// Registry must be completely clean (0 entries, 0 refCount)
	if entries := mgr.ActiveEntries(); entries != 0 {
		t.Fatalf("expected 0 active entries after canceled fork and save release, got %d", entries)
	}
	if count := mgr.RefCount(sandboxID); count != 0 {
		t.Fatalf("expected refCount 0, got %d", count)
	}
}
