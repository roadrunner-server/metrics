package metrics

import (
	"testing"

	"tests/helpers"

	"github.com/roadrunner-server/metrics/v6"
	rpcPlugin "github.com/roadrunner-server/rpc/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsDifferentRPCCalls(t *testing.T) {
	const (
		metricsURL = "http://[::1]:2114/metrics"
		rpcAddr    = "127.0.0.1:6352"
	)

	rr, stop := helpers.Start(t, "configs/.rr-metrics-different-rpc-calls.yaml", []any{
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithProbe(metricsURL))

	helpers.WaitListener(t, "tcp", rpcAddr)

	t.Run("DeclareMetric", declareMetricsTest(rpcAddr))
	assert.Contains(t, helpers.Scrape(t, metricsURL), "test_metrics_named_collector")

	t.Run("AddMetric", addMetricsTest(rpcAddr))
	assert.Contains(t, helpers.Scrape(t, metricsURL), "test_metrics_named_collector 10000")

	t.Run("SetMetric", setMetric(rpcAddr))
	assert.Contains(t, helpers.Scrape(t, metricsURL), "user_gauge_collector 100")

	t.Run("VectorMetric", vectorMetric(rpcAddr))
	assert.Contains(t, helpers.Scrape(t, metricsURL), `gauge_2_collector{section="first",type="core"} 100`)

	t.Run("MissingSection", missingSection(rpcAddr))
	t.Run("SetWithoutLabels", setWithoutLabels(rpcAddr))
	t.Run("SetOnHistogram", setOnHistogram(rpcAddr))

	t.Run("MetricSub", subMetric(rpcAddr))
	assert.Contains(t, helpers.Scrape(t, metricsURL), "sub_gauge_subMetric 1")

	t.Run("SubVector", subVector(rpcAddr))
	assert.Contains(t, helpers.Scrape(t, metricsURL), `sub_gauge_subVector{section="first",type="core"} 1`)

	t.Run("RegisterHistogram", registerHistogram(rpcAddr))

	histogramOut := helpers.Scrape(t, metricsURL)
	assert.Contains(t, histogramOut, `TYPE histogram_registerHistogram`)
	assert.Contains(t, histogramOut, `histogram_registerHistogram_bucket{le="0.1"} 0`)
	assert.Contains(t, histogramOut, `histogram_registerHistogram_bucket{le="0.2"} 0`)
	assert.Contains(t, histogramOut, `histogram_registerHistogram_bucket{le="0.5"} 0`)
	assert.Contains(t, histogramOut, `histogram_registerHistogram_bucket{le="+Inf"} 0`)
	assert.Contains(t, histogramOut, `histogram_registerHistogram_sum 0`)
	assert.Contains(t, histogramOut, `histogram_registerHistogram_count 0`)

	t.Run("CounterMetric", counterMetric(rpcAddr))
	counterOut := helpers.Scrape(t, metricsURL)
	assert.Contains(t, counterOut, "HELP default_default_counter_CounterMetric test_counter")
	assert.Contains(t, counterOut, `default_default_counter_CounterMetric{section="section2",type="type2"}`)

	t.Run("ObserveMetric", observeMetric(rpcAddr))
	assert.Contains(t, helpers.Scrape(t, metricsURL), "observe_observeMetric")

	t.Run("ObserveMetricNotEnoughLabels", observeMetricNotEnoughLabels(rpcAddr))

	t.Run("ConfiguredCounterMetric", configuredCounterMetric(rpcAddr))
	configuredOut := helpers.Scrape(t, metricsURL)
	assert.Contains(t, configuredOut, "HELP app_metric_counter Custom application counter.")
	assert.Contains(t, configuredOut, `app_metric_counter 100`)

	stop()

	require.Equal(t, 0, rr.Logs.FilterMessageSnippet("http server was started").Len())
	require.Equal(t, 0, rr.Logs.FilterMessageSnippet("http log").Len())

	require.Equal(t, 6, rr.Logs.FilterMessageSnippet("adding metric").Len())
	require.Equal(t, 17, rr.Logs.FilterMessageSnippet("metric successfully added").Len())
	require.Equal(t, 12, rr.Logs.FilterMessageSnippet("declaring new metric").Len())
	require.Equal(t, 2, rr.Logs.FilterMessageSnippet("observing metric").Len())
	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("observe operation finished successfully").Len())

	require.Equal(t, 2, rr.Logs.FilterMessageSnippet("set operation finished successfully").Len())
	require.Equal(t, 2, rr.Logs.FilterMessageSnippet("subtracting value from metric").Len())
	require.Equal(t, 2, rr.Logs.FilterMessageSnippet("subtracting operation finished successfully").Len())
	require.Equal(t, 2, rr.Logs.FilterMessageSnippet("failed to get metrics with label values").Len())
	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("required labels for collector").Len())
}

func TestUnregister(t *testing.T) {
	const (
		metricsURL = "http://[::1]:2117/metrics"
		rpcAddr    = "127.0.0.1:6353"
	)

	rr, stop := helpers.Start(t, "configs/.rr-metrics-unregister.yaml", []any{
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithProbe(metricsURL))

	helpers.WaitListener(t, "tcp", rpcAddr)

	t.Run("DeclareMetric", declareMetricsTest(rpcAddr))
	require.Contains(t, helpers.Scrape(t, metricsURL), "test_metrics_named_collector")

	t.Run("UnregisterMetric", unregisterMetric("test_metrics_named_collector", rpcAddr))
	require.NotContains(t, helpers.Scrape(t, metricsURL), "test_metrics_named_collector")

	// the collector is gone from the plugin map, so the second call cannot find it
	t.Run("UnregisterUnknownMetric", unregisterUnknownMetric("test_metrics_named_collector", rpcAddr))

	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("collector was successfully unregistered").Len())

	stop()
}

func TestUpsertOfMetricsDeclaration(t *testing.T) {
	const (
		metricsURL = "http://[::1]:2117/metrics"
		rpcAddr    = "127.0.0.1:6353"
	)

	rr, stop := helpers.Start(t, "configs/.rr-metrics-unregister.yaml", []any{
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
	}, helpers.WithObservedLogger(), helpers.WithProbe(metricsURL))

	helpers.WaitListener(t, "tcp", rpcAddr)

	t.Run("DeclareMetric", declareMetricsTest(rpcAddr))
	require.Contains(t, helpers.Scrape(t, metricsURL), "test_metrics_named_collector")

	t.Run("RedeclareMetric", declareMetricsTest(rpcAddr))
	require.Equal(t, 1, rr.Logs.FilterMessageSnippet("metric with provided name already exist").Len())

	stop()
}

func configuredCounterMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)
		var addOk bool
		err := client.Call("metrics.Add", metrics.Metric{Name: "app_metric_counter", Value: 100.0}, &addOk)
		assert.NoError(t, err)
		assert.True(t, addOk)
	}
}

func observeMetricNotEnoughLabels(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "observe_observeMetricNotEnoughLabels",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Help:      "test_observe",
				Type:      metrics.Histogram,
				Labels:    []string{"type", "section"},
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		// two-label collector with one label value, prometheus rejects the lookup
		var observeOk bool
		err = client.Call("metrics.Observe", metrics.Metric{Name: "observe_observeMetricNotEnoughLabels", Value: 100.0, Labels: []string{"test"}}, &observeOk)
		assert.Error(t, err)
	}
}

func observeMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "observe_observeMetric",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Help:      "test_observe",
				Type:      metrics.Histogram,
				Labels:    []string{"type", "section"},
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		var observeOk bool
		err = client.Call("metrics.Observe", metrics.Metric{Name: "observe_observeMetric", Value: 100.0, Labels: []string{"test", "test2"}}, &observeOk)
		assert.NoError(t, err)
		assert.True(t, observeOk)
	}
}

func counterMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "counter_CounterMetric",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Help:      "test_counter",
				Type:      metrics.Counter,
				Labels:    []string{"type", "section"},
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		var addOk bool
		err = client.Call("metrics.Add", metrics.Metric{Name: "counter_CounterMetric", Value: 100.0, Labels: []string{"type2", "section2"}}, &addOk)
		assert.NoError(t, err)
		assert.True(t, addOk)
	}
}

func registerHistogram(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "histogram_registerHistogram",
			Collector: metrics.Collector{
				Help:    "test_histogram",
				Type:    metrics.Histogram,
				Buckets: []float64{0.1, 0.2, 0.5},
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		// Histogram doesn't support Add — must surface as an error.
		var addOk bool
		err = client.Call("metrics.Add", metrics.Metric{Name: "histogram_registerHistogram", Value: 10000}, &addOk)
		assert.Error(t, err)
	}
}

func subVector(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "sub_gauge_subVector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
				Labels:    []string{"type", "section"},
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		var addOk bool
		err = client.Call("metrics.Add", metrics.Metric{Name: "sub_gauge_subVector", Value: 100000, Labels: []string{"core", "first"}}, &addOk)
		assert.NoError(t, err)
		assert.True(t, addOk)

		var subOk bool
		err = client.Call("metrics.Sub", metrics.Metric{Name: "sub_gauge_subVector", Value: 99999, Labels: []string{"core", "first"}}, &subOk)
		assert.NoError(t, err)
		assert.True(t, subOk)
	}
}

func subMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "sub_gauge_subMetric",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		var addOk bool
		err = client.Call("metrics.Add", metrics.Metric{Name: "sub_gauge_subMetric", Value: 100000}, &addOk)
		assert.NoError(t, err)
		assert.True(t, addOk)

		var subOk bool
		err = client.Call("metrics.Sub", metrics.Metric{Name: "sub_gauge_subMetric", Value: 99999}, &subOk)
		assert.NoError(t, err)
		assert.True(t, subOk)
	}
}

func setOnHistogram(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "histogram_setOnHistogram",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Histogram,
				Labels:    []string{"type", "section"},
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		// Histogram does not support Set — must surface as an error.
		var setOk bool
		err = client.Call("metrics.Set", metrics.Metric{Name: "histogram_setOnHistogram", Value: 100.0}, &setOk)
		assert.Error(t, err)
	}
}

func setWithoutLabels(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "gauge_setWithoutLabels",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
				Labels:    []string{"type", "section"},
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		// GaugeVec requires labels — Set with empty labels must error.
		var setOk bool
		err = client.Call("metrics.Set", metrics.Metric{Name: "gauge_setWithoutLabels", Value: 100.0}, &setOk)
		assert.Error(t, err)
	}
}

func missingSection(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "gauge_missing_section_collector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
				Labels:    []string{"type", "section"},
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		// Two-label collector with one label value — prometheus rejects the
		// call, surfaces as an error on the wire.
		var setOk bool
		err = client.Call("metrics.Set", metrics.Metric{Name: "gauge_missing_section_collector", Value: 100.0, Labels: []string{"missing"}}, &setOk)
		assert.Error(t, err)
	}
}

func vectorMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "gauge_2_collector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
				Labels:    []string{"type", "section"},
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		var setOk bool
		err = client.Call("metrics.Set", metrics.Metric{Name: "gauge_2_collector", Value: 100.0, Labels: []string{"core", "first"}}, &setOk)
		assert.NoError(t, err)
		assert.True(t, setOk)
	}
}

func setMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)

		nc := metrics.NamedCollector{
			Name: "user_gauge_collector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)

		var setOk bool
		err = client.Call("metrics.Set", metrics.Metric{Name: "user_gauge_collector", Value: 100.0}, &setOk)
		assert.NoError(t, err)
		assert.True(t, setOk)
	}
}

func addMetricsTest(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)
		var addOk bool
		err := client.Call("metrics.Add", metrics.Metric{Name: "test_metrics_named_collector", Value: 10000}, &addOk)
		assert.NoError(t, err)
		assert.True(t, addOk)
	}
}

func declareMetricsTest(address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)
		nc := metrics.NamedCollector{
			Name: "test_metrics_named_collector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Counter,
				Help:      "NO HELP!",
			},
		}

		var declareOk bool
		err := client.Call("metrics.Declare", nc, &declareOk)
		assert.NoError(t, err)
		assert.True(t, declareOk)
	}
}

func unregisterMetric(name string, address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)
		var unregisterOk bool
		err := client.Call("metrics.Unregister", name, &unregisterOk)
		assert.NoError(t, err)
		assert.True(t, unregisterOk)
	}
}

func unregisterUnknownMetric(name string, address string) func(t *testing.T) {
	return func(t *testing.T) {
		client := helpers.RPC(t, address)
		var unregisterOk bool
		err := client.Call("metrics.Unregister", name, &unregisterOk)
		assert.Error(t, err)
		assert.False(t, unregisterOk)
	}
}
