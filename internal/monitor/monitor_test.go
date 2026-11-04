package monitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nebulacsf/ncsf-sentinel/internal/config"
	"github.com/nebulacsf/ncsf-sentinel/internal/sshx"
)

type fakeCollector struct {
	delay time.Duration

	mu      sync.Mutex
	active  int
	maxSeen int
}

func (f *fakeCollector) Collect(ctx context.Context, srv config.Server) sshx.Result {
	f.mu.Lock()
	f.active++
	if f.active > f.maxSeen {
		f.maxSeen = f.active
	}
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return sshx.Result{Server: srv, Status: sshx.StatusOffline, Err: ctx.Err(), Reason: "canceled"}
	case <-time.After(f.delay):
		return sshx.Result{Server: srv, Status: sshx.StatusOnline}
	}
}

func (f *fakeCollector) maxConcurrency() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxSeen
}

func TestCollectAllPreservesOrderAndLimit(t *testing.T) {
	servers := make([]config.Server, 12)
	for i := range servers {
		servers[i] = config.Server{Name: string(rune('a' + i)), Host: "host", User: "user", Port: 22}
	}
	fake := &fakeCollector{delay: 10 * time.Millisecond}
	results := CollectAll(context.Background(), fake, servers, 3)

	if len(results) != len(servers) {
		t.Fatalf("got %d results", len(results))
	}
	for i, r := range results {
		if r.Server.Name != servers[i].Name {
			t.Fatalf("result %d = %q, want %q", i, r.Server.Name, servers[i].Name)
		}
		if r.Status != sshx.StatusOnline {
			t.Fatalf("result %d status = %v", i, r.Status)
		}
	}
	if got := fake.maxConcurrency(); got > 3 {
		t.Fatalf("max concurrency = %d, want <= 3", got)
	}
}

func TestCollectAllCanceled(t *testing.T) {
	servers := []config.Server{
		{Name: "a", Host: "h", User: "u", Port: 22},
		{Name: "b", Host: "h", User: "u", Port: 22},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan []sshx.Result, 1)
	go func() {
		done <- CollectAll(ctx, &fakeCollector{delay: time.Second}, servers, 1)
	}()

	select {
	case results := <-done:
		if len(results) != 2 {
			t.Fatalf("got %d results", len(results))
		}
		for _, r := range results {
			if r.Status != sshx.StatusOffline {
				t.Fatalf("status = %v, want offline", r.Status)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("CollectAll did not respect cancellation")
	}
}

func TestCollectAllEmpty(t *testing.T) {
	results := CollectAll(context.Background(), &fakeCollector{}, nil, 4)
	if len(results) != 0 {
		t.Fatalf("got %d results", len(results))
	}
}
