// Package refresher provides a small generic primitive for atomically
// reloading a slice of rules from the database on a fixed cadence.
//
// Why not just patch each provider?
//
//	denoise / suppress / mute each define their own *RuleProvider interface.
//	A separate Refreshing*Provider per package would duplicate the goroutine,
//	the atomic.Pointer plumbing, and the error handling three times. The
//	generic Refresher below holds the only copy of that logic; each module
//	wires it in by writing a 3-line adapter that satisfies its own provider
//	interface.
//
// Failure model
//
//	A failed reload (DB hiccup, malformed rule) keeps the previous snapshot
//	live. The system never serves "no rules" because of a transient query
//	error — that would silently disable denoise mid-incident, which is
//	exactly when operators expect it most. Only an EMPTY-but-successful
//	query result is allowed to clear the cache.
package refresher

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/toolkits/pkg/logger"
)

// Loader is the per-tick database query function. It returns a fresh slice
// (or nil + error). Slice contents must be immutable from the caller's
// perspective once handed to the refresher — concurrent readers may
// inspect any field without locking.
type Loader[T any] func(ctx context.Context) ([]T, error)

// Refresher wraps a periodically-refreshed slice. T is the element type
// (e.g. *CompiledCronRule, *customModels.CustomAggregateRule).
//
// Read path is lock-free: Get returns whatever the last successful Load
// produced. Write path runs in a single goroutine started by Start.
type Refresher[T any] struct {
	name     string              // for log prefixes
	interval time.Duration       // refresh cadence
	loader   Loader[T]           // pulled on each tick
	current  atomic.Pointer[[]T] // last successful snapshot
	cancel   context.CancelFunc  // stops the background goroutine
}

// New constructs a Refresher. Call Start to begin background reloads.
//
// The interval is taken as-is; pick something appropriate for the data —
// rules tend to drift slowly (minutes to days), so 5s is a fine default,
// while a kill-switch like emergency-mute deserves 1s for a snappier
// turn-on. Sub-second intervals are rejected (caller bug).
func New[T any](name string, interval time.Duration, loader Loader[T]) *Refresher[T] {
	if interval < time.Second {
		interval = time.Second
	}
	r := &Refresher[T]{name: name, interval: interval, loader: loader}
	empty := []T{}
	r.current.Store(&empty)
	return r
}

// Start kicks off the background goroutine. The first reload happens
// synchronously so callers can observe the initial state before Start
// returns — useful when bootstrap wants to log "loaded N rules" up front.
func (r *Refresher[T]) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel

	r.tick(ctx) // synchronous warm-up

	go func() {
		t := time.NewTicker(r.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.tick(ctx)
			}
		}
	}()
}

// Stop cancels the background goroutine. Idempotent.
func (r *Refresher[T]) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

// Get returns the current snapshot. Hot-path call; never blocks.
func (r *Refresher[T]) Get() []T {
	if p := r.current.Load(); p != nil {
		return *p
	}
	return nil
}

// tick performs one reload attempt. On error the previous snapshot is
// left in place; on success the new slice is stored, even if empty.
func (r *Refresher[T]) tick(ctx context.Context) {
	rows, err := r.loader(ctx)
	if err != nil {
		logger.Warningf("refresher[%s]: reload failed (keeping previous snapshot): %v", r.name, err)
		return
	}
	r.current.Store(&rows)
}
