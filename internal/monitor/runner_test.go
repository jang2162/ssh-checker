package monitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type memSink struct {
	mu      sync.Mutex
	saved   map[int64][]Result
	failFor int64
}

func (s *memSink) SaveResults(_ context.Context, id int64, rs []Result) error {
	if id == s.failFor {
		return errors.New("db down")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved[id] = rs
	return nil
}

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestRunnerCheckAll(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"a.test"}}), nil)
	sink := &memSink{saved: map[int64][]Result{}, failFor: 2}
	r := &Runner{Prober: &Prober{Roots: ca.pool}, Sink: sink, Log: discard}
	t1 := target(port, "a.test", "127.0.0.1")
	t2 := t1
	t2.ID = 2
	if failed := r.CheckAll(context.Background(), []Target{t1, t2}); failed != 1 {
		t.Fatalf("failed = %d", failed)
	}
	if rs := sink.saved[1]; len(rs) != 1 || rs[0].Status != StatusOK {
		t.Fatalf("saved = %+v", sink.saved)
	}

	rs, err := r.Check(context.Background(), t2)
	if err == nil || len(rs) != 1 {
		t.Fatalf("Check = %v, %v", rs, err)
	}
}

func TestRunnerCheckAllRunsConcurrently(t *testing.T) {
	port := hangingPort(t)
	var ts []Target
	for i := range 4 {
		tg := target(port, "a.test", "127.0.0.1")
		tg.ID = int64(i + 1)
		ts = append(ts, tg)
	}
	sink := &memSink{saved: map[int64][]Result{}}
	r := &Runner{Prober: &Prober{Timeout: 300 * time.Millisecond}, Sink: sink, Concurrency: 4, Log: discard}
	start := time.Now()
	r.CheckAll(context.Background(), ts)
	if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
		t.Fatalf("CheckAll took %s; targets did not run concurrently", elapsed)
	}
	if len(sink.saved) != 4 {
		t.Fatalf("saved %d targets", len(sink.saved))
	}
}
