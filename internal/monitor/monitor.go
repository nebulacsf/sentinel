// Package monitor fans out metric collection across servers.
package monitor

import (
	"context"
	"time"

	"github.com/nebulacsf/ncsf-sentinel/internal/config"
	"github.com/nebulacsf/ncsf-sentinel/internal/sshx"
)

// DefaultParallelism bounds simultaneous SSH connections.
const DefaultParallelism = 16

// Collector is implemented by sshx.Collector and by test/demo fakes.
type Collector interface {
	Collect(context.Context, config.Server) sshx.Result
}

type indexedResult struct {
	index  int
	result sshx.Result
}

// CollectAll collects every server concurrently and returns results in the
// same order as servers. At most parallel collections run at once. A
// cancellation produces OFFLINE results for the servers that did not run.
func CollectAll(ctx context.Context, collector Collector, servers []config.Server, parallel int) []sshx.Result {
	results := make([]sshx.Result, len(servers))
	if len(servers) == 0 {
		return results
	}
	if parallel <= 0 {
		parallel = DefaultParallelism
	}
	if parallel > len(servers) {
		parallel = len(servers)
	}

	sem := make(chan struct{}, parallel)
	out := make(chan indexedResult, len(servers))

	for i, srv := range servers {
		go func(index int, server config.Server) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				out <- indexedResult{index, sshx.Result{
					Server:      server,
					Status:      sshx.StatusOffline,
					Reason:      "canceled",
					Err:         ctx.Err(),
					CollectedAt: time.Now(),
				}}
				return
			}
			out <- indexedResult{index, collector.Collect(ctx, server)}
		}(i, srv)
	}

	for range servers {
		item := <-out
		results[item.index] = item.result
	}
	return results
}
