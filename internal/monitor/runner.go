package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Sink는 점검 결과를 저장한다. store.Store가 구현한다.
type Sink interface {
	SaveResults(ctx context.Context, targetID int64, rs []Result) error
}

const DefaultConcurrency = 8

type Runner struct {
	Prober      *Prober
	Sink        Sink
	Concurrency int // 0이면 DefaultConcurrency
	Log         *slog.Logger
}

// Check는 대상 하나를 점검하고 저장한다. 저장에 실패해도 결과는 돌려준다.
func (r *Runner) Check(ctx context.Context, t Target) ([]Result, error) {
	rs := r.Prober.Probe(ctx, t)
	if err := r.Sink.SaveResults(ctx, t.ID, rs); err != nil {
		return rs, fmt.Errorf("monitor: save target %d: %w", t.ID, err)
	}
	return rs, nil
}

// CheckAll은 대상들을 최대 Concurrency개씩 동시에 점검한다. 저장에 실패한 대상은 로그를 남기고 센다.
func (r *Runner) CheckAll(ctx context.Context, ts []Target) (failed int) {
	n := r.Concurrency
	if n <= 0 {
		n = DefaultConcurrency
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, t := range ts {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			if _, err := r.Check(ctx, t); err != nil {
				r.Log.Error("tls check", "target", t.ID, "err", err)
				mu.Lock()
				failed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return failed
}
