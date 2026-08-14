package metrics

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"

	"tests/helpers"

	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/metrics/v6"
	"github.com/roadrunner-server/prometheus/v6"
	rpcPlugin "github.com/roadrunner-server/rpc/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPMetrics(t *testing.T) {
	const (
		metricsURL = "http://127.0.0.1:2115/metrics"
		httpAddr   = "127.0.0.1:13223"
	)

	rr, stop := helpers.Start(t, "configs/.rr-metrics-http.yaml", []any{
		&metrics.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&prometheus.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithProbe(metricsURL))

	// a tcp wait, not a request: the access log records every handled request
	// and the count below is exact
	helpers.WaitListener(t, "tcp", httpAddr)

	require.NoError(t, echoRequest(t.Context(), httpAddr))
	require.NoError(t, echoRequest(t.Context(), httpAddr))

	// the middleware records the request after the response is flushed
	helpers.RequireScrapeContains(t, metricsURL, `rr_http_request_total{status="200"}`)

	out := helpers.Scrape(t, metricsURL)
	assert.Contains(t, out, `rr_http_request_duration_seconds_bucket`)
	assert.Contains(t, out, `rr_http_request_duration_seconds_sum{status="200"}`)
	assert.Contains(t, out, `rr_http_request_duration_seconds_count{status="200"}`)
	assert.Contains(t, out, "rr_http_workers_memory_bytes")
	assert.Contains(t, out, `state="ready"}`)
	assert.Contains(t, out, `{pid=`)
	assert.Contains(t, out, `rr_http_total_workers 10`)

	stop()

	require.Equal(t, 2, rr.Logs.FilterMessageSnippet("http log").Len())
}

func TestHTTPMetricsNoFreeWorkers(t *testing.T) {
	const (
		metricsURL = "http://127.0.0.1:2116/metrics"
		httpAddr   = "127.0.0.1:15442"
	)

	_, stop := helpers.Start(t, "configs/.rr-http-metrics-no-free-workers.yaml", []any{
		&metrics.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&prometheus.Plugin{},
	}, helpers.WithProbe(metricsURL))

	helpers.WaitListener(t, "tcp", httpAddr)

	// the single worker sleeps for the whole request, so it is busy until this
	// goroutine is joined
	busy := &sync.WaitGroup{}
	busy.Go(func() {
		if err := echoRequest(t.Context(), httpAddr); err != nil {
			t.Errorf("request holding the worker: %v", err)
		}
	})

	helpers.RequireScrapeContains(t, metricsURL, "rr_http_requests_queue 1")

	// the pool has no worker left, so this one is rejected after allocate_timeout
	require.NoError(t, echoRequest(t.Context(), httpAddr))
	helpers.RequireScrapeContains(t, metricsURL, "rr_http_no_free_workers_total 1")

	// the container may only be torn down once the worker is free again
	busy.Wait()
	stop()
}

func TestMetricsIssue571(t *testing.T) {
	const metricsURL = "http://127.0.0.1:23557/metrics"

	_, stop := helpers.Start(t, "configs/.rr-issue-571.yaml", []any{
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithProbe(metricsURL))

	// the workers declare the collector over rpc while they boot
	helpers.RequireScrapeContains(t, metricsURL, "# TYPE test counter")

	stop()
}

// echoRequest issues a GET to the RoadRunner http frontend and drains the body.
// It returns an error instead of failing the test so that it can also be called
// from a goroutine.
func echoRequest(ctx context.Context, addr string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	_, err = io.ReadAll(resp.Body)

	return err
}
