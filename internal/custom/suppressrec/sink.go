// Package suppressrec provides an asynchronous, failure-tolerant sink for
// suppressed-event records.
//
// Why isolated from suppress/?
//
//	suppress/ is a hot-path matcher; it must NEVER allocate goroutines or
//	touch DB itself. We hand it a Sink interface that is a pure non-blocking
//	channel push — even if the DB is down the matcher proceeds normally.
//	This package owns the goroutine + DB + batching that the suppress
//	package opts out of.
//
// Backpressure model:
//
//	Hot-path entry RecordSuppression(...) does ch <- row with default. If
//	the channel is full the record is dropped + counter incremented. The
//	alert pipeline stays unblocked, and the audit table is best-effort by
//	design (it is a "nice to have" page, not a correctness guarantee).
package suppressrec

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/internal/custom/suppress"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"

	"github.com/toolkits/pkg/logger"
)

// asyncSink is the production implementation of suppress.RecordSink.
// Construct with NewAsyncSink and call Start() once during bootstrap.
type asyncSink struct {
	ch          chan *customModels.CustomSuppressedEvent
	ctx         *ctx.Context
	batchSize   int
	flushPeriod time.Duration

	dropped atomic.Int64
	flushed atomic.Int64

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Compile-time check: asyncSink satisfies the interface the suppress
// package's HookAdapter expects via SetRecordSink.
var _ suppress.RecordSink = (*asyncSink)(nil)

// NewAsyncSink builds a sink with the given buffer / batch / flush settings.
//
// Sizing notes (defaults are usually fine):
//
//	bufferSize  — max in-flight records. 4096 absorbs ~10s of a 400 evt/s
//	              storm at the suppression layer; bigger is just memory.
//	batchSize   — how many records per INSERT. 100 is a sweet spot for
//	              MySQL: small enough to fit in one write txn, big enough
//	              to amortize the round-trip.
//	flushPeriod — max wait before flushing a partial batch. 1s makes the
//	              page feel "live" without thrashing the DB on idle clusters.
func NewAsyncSink(c *ctx.Context, bufferSize, batchSize int, flushPeriod time.Duration) *asyncSink {
	if bufferSize <= 0 {
		bufferSize = 4096
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	if flushPeriod <= 0 {
		flushPeriod = time.Second
	}
	return &asyncSink{
		ch:          make(chan *customModels.CustomSuppressedEvent, bufferSize),
		ctx:         c,
		batchSize:   batchSize,
		flushPeriod: flushPeriod,
	}
}

// RecordSuppression implements suppress.RecordSink. Hot-path call:
// non-blocking. If the buffer is full we drop and bump the counter; the
// matcher proceeds either way.
func (s *asyncSink) RecordSuppression(rec *suppress.SuppressionRecord) {
	if rec == nil {
		return
	}
	row := toRow(rec)
	if row.SuppressedAt == 0 {
		row.SuppressedAt = time.Now().Unix()
	}
	select {
	case s.ch <- row:
	default:
		s.dropped.Add(1)
	}
}

// toRow maps the cycle-free SuppressionRecord (defined in the suppress
// package) to the GORM model row. Centralizing this here keeps the
// suppress package free of DB types.
func toRow(rec *suppress.SuppressionRecord) *customModels.CustomSuppressedEvent {
	return &customModels.CustomSuppressedEvent{
		RuleId:          rec.RuleId,
		RuleName:        rec.RuleName,
		SourceEventHash: rec.SourceEventHash,
		TargetEventHash: rec.TargetEventHash,
		SourceRuleName:  rec.SourceRuleName,
		TargetRuleName:  rec.TargetRuleName,
		SourceTags:      rec.SourceTags,
		TargetTags:      rec.TargetTags,
		SourceSeverity:  rec.SourceSeverity,
		TargetSeverity:  rec.TargetSeverity,
		GroupId:         rec.GroupId,
		DatasourceId:    rec.DatasourceId,
		SuppressedAt:    rec.SuppressedAt,
	}
}

// Start launches the worker goroutine. Idempotent? No — call once.
func (s *asyncSink) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(1)
	go s.run(ctx)
}

// Stop drains the buffer and exits. Useful in tests; in production the
// app context cancels and the worker shuts down naturally.
func (s *asyncSink) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// Stats exposes counters for an /metrics-style observability hook later.
func (s *asyncSink) Stats() (dropped, flushed int64) {
	return s.dropped.Load(), s.flushed.Load()
}

func (s *asyncSink) run(ctx context.Context) {
	defer s.wg.Done()

	buf := make([]*customModels.CustomSuppressedEvent, 0, s.batchSize)
	tick := time.NewTicker(s.flushPeriod)
	defer tick.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		batch := buf
		buf = make([]*customModels.CustomSuppressedEvent, 0, s.batchSize)
		s.insertBatch(batch)
	}

	for {
		select {
		case <-ctx.Done():
			drain := true
			for drain {
				select {
				case rec := <-s.ch:
					buf = append(buf, rec)
					if len(buf) >= s.batchSize {
						flush()
					}
				default:
					drain = false
				}
			}
			flush()
			return

		case rec := <-s.ch:
			buf = append(buf, rec)
			if len(buf) >= s.batchSize {
				flush()
			}

		case <-tick.C:
			flush()
		}
	}
}

func (s *asyncSink) insertBatch(batch []*customModels.CustomSuppressedEvent) {
	if len(batch) == 0 {
		return
	}
	if err := models.DB(s.ctx).CreateInBatches(batch, s.batchSize).Error; err != nil {
		logger.Errorf("suppressrec: failed to insert %d records: %v", len(batch), err)
		return
	}
	s.flushed.Add(int64(len(batch)))
}
