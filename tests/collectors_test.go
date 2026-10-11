package metrics

import (
	"testing"

	"tests/helpers"

	"github.com/roadrunner-server/metrics/v6"
	rpcPlugin "github.com/roadrunner-server/rpc/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsInit(t *testing.T) {
	const metricsURL = "http://[::1]:2112/metrics"

	helpers.Start(t, "configs/.rr-metrics-init.yaml", []any{
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
		&GaugeStatProvider{},
	}, helpers.WithProbe(metricsURL))

	out := helpers.Scrape(t, metricsURL)
	assert.Contains(t, out, "go_gc_duration_seconds")
	assert.Contains(t, out, "app_metric_counter")
}

func TestMetricsGaugeCollector(t *testing.T) {
	const metricsURL = "http://[::1]:2113/metrics"

	helpers.Start(t, "configs/.rr-metrics-gauge.yaml", []any{
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
		&GaugeStatProvider{},
	}, helpers.WithProbe(metricsURL))

	out := helpers.Scrape(t, metricsURL)
	assert.Contains(t, out, "my_gauge 100")
	assert.Contains(t, out, "my_gauge2 100")

	// the provided gauges are registered once and collected on every scrape,
	// next to the process and go collectors
	out = helpers.Scrape(t, metricsURL)
	assert.Contains(t, out, "my_gauge 100")
	assert.Contains(t, out, "go_gc_duration_seconds")
}

func TestMetricsPrefixAndLabels(t *testing.T) {
	const metricsURL = "http://[::1]:2118/metrics"

	helpers.Start(t, "configs/.rr-metrics-prefix-labels.yaml", []any{
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
		&GaugeStatProvider{},
	}, helpers.WithProbe(metricsURL))

	out := helpers.Scrape(t, metricsURL)
	assert.Contains(t, out, `myapp_my_gauge{service="test"} 100`)
	assert.Contains(t, out, `myapp_my_gauge2{service="test"} 100`)
	assert.NotContains(t, out, "\nmy_gauge 100")

	// the go, process and application collectors keep their names and labels
	assert.Contains(t, out, "\ngo_goroutines ")
	assert.Contains(t, out, "\nprocess_open_fds ")
	assert.Contains(t, out, "\napp_metric_counter 0")
}

func TestMetricsUnknownCollectorType(t *testing.T) {
	err := helpers.StartExpectInitError(t, "configs/.rr-metrics-unknown-collector.yaml", []any{
		&metrics.Plugin{},
	})

	require.ErrorContains(t, err, "invalid metric type `gaugee` for `app_metric`")
}
