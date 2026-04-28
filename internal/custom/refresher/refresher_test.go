package refresher

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestRefresher_InitialLoadObservedSynchronously verifies that Start
// performs a synchronous warm-up: by the time Start returns, Get already
// reflects the first loader call.
func TestRefresher_InitialLoadObservedSynchronously(t *testing.T) {
	loader := func(_ context.Context) ([]int, error) {
		return []int{1, 2, 3}, nil
	}
	r := New("test", time.Hour, loader) // huge interval — only the synchronous warm-up should fire
	defer r.Stop()

	r.Start(context.Background())

	got := r.Get()
	if len(got) != 3 || got[0] != 1 {
		t.Fatalf("expected [1,2,3] after Start, got %v", got)
	}
}

// TestRefresher_BackgroundReloadsKeepCacheFresh asserts that the ticker
// path swaps in a new snapshot. Uses a 1-second interval (the minimum
// the refresher allows) to verify the background ticker actually fires.
func TestRefresher_BackgroundReloadsKeepCacheFresh(t *testing.T) {
	if testing.Short() {
		t.Skip("background-tick test takes ~1.5s")
	}

	var calls atomic.Int32
	loader := func(_ context.Context) ([]int, error) {
		n := calls.Add(1)
		return []int{int(n)}, nil
	}
	r := New("test", time.Second, loader)
	defer r.Stop()

	r.Start(context.Background())

	// Synchronous warm-up was call #1. Wait long enough for at least
	// one more tick (interval=1s).
	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if calls.Load() >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if calls.Load() < 2 {
		t.Fatalf("expected at least 2 loader calls; got %d", calls.Load())
	}
	got := r.Get()
	if len(got) == 0 || got[0] < 2 {
		t.Fatalf("Get returned stale snapshot: %v", got)
	}
}

// TestRefresher_LoaderErrorKeepsPreviousSnapshot is the critical
// availability property: a transient DB failure must NOT clear the
// cache. Otherwise denoise rules vanish during exactly the kind of
// outage they were configured for.
func TestRefresher_LoaderErrorKeepsPreviousSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real ticker, takes ~2s")
	}

	var phase atomic.Int32 // 0 = good, 1 = error
	loader := func(_ context.Context) ([]int, error) {
		if phase.Load() == 0 {
			return []int{42}, nil
		}
		return nil, errors.New("simulated DB outage")
	}
	r := New("test", time.Second, loader)
	defer r.Stop()

	r.Start(context.Background())
	if got := r.Get(); len(got) != 1 || got[0] != 42 {
		t.Fatalf("warm-up snapshot wrong: %v", got)
	}

	// Flip to error mode and let several ticks fail.
	phase.Store(1)
	time.Sleep(2500 * time.Millisecond)

	got := r.Get()
	if len(got) != 1 || got[0] != 42 {
		t.Fatalf("loader error must not clear cache; got %v", got)
	}
}

// TestRefresher_StopHaltsBackgroundLoader verifies Stop ends the
// goroutine — important so a refresher tied to a unit test doesn't
// leak across tests.
func TestRefresher_StopHaltsBackgroundLoader(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real ticker, takes ~3s")
	}

	var calls atomic.Int32
	loader := func(_ context.Context) ([]int, error) {
		calls.Add(1)
		return nil, nil
	}
	r := New("test", time.Second, loader)

	r.Start(context.Background())
	time.Sleep(1500 * time.Millisecond) // at least one tick after warm-up
	r.Stop()

	snapshot := calls.Load()
	time.Sleep(1500 * time.Millisecond)

	if calls.Load() != snapshot {
		t.Fatalf("loader called after Stop: before=%d after=%d", snapshot, calls.Load())
	}
}

// TestRefresher_SubSecondIntervalClampedToOneSecond — guards a foot-gun.
// Sub-second polling under the suppress/cron table sizes we expect would
// be wasteful; bumping it to 1s is a deliberate floor.
func TestRefresher_SubSecondIntervalClampedToOneSecond(t *testing.T) {
	r := New("test", 10*time.Millisecond, func(_ context.Context) ([]int, error) {
		return nil, nil
	})
	defer r.Stop()
	if r.interval < time.Second {
		t.Fatalf("expected sub-second interval to be clamped; got %v", r.interval)
	}
}
