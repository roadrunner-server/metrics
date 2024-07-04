package metrics

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	mocklogger "tests/mock"

	"github.com/roadrunner-server/config/v4"
	"github.com/roadrunner-server/endure/v2"
	goridgeRpc "github.com/roadrunner-server/goridge/v3/pkg/rpc"
	httpPlugin "github.com/roadrunner-server/http/v4"
	"github.com/roadrunner-server/logger/v4"
	"github.com/roadrunner-server/metrics/v5"
	"github.com/roadrunner-server/prometheus/v4"
	rpcPlugin "github.com/roadrunner-server/rpc/v4"
	"github.com/roadrunner-server/server/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const dialNetwork = "tcp"

func TestMetricsInit(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2024.2.0",
		Path:    "configs/.rr-metrics-init.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
		&logger.Plugin{},
		&Plugin1{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	time.Sleep(time.Second * 2)
	out, err := getIPV6("http://[::1]:2112/metrics")
	assert.NoError(t, err)

	assert.Contains(t, out, "go_gc_duration_seconds")
	assert.Contains(t, out, "app_metric_counter")

	wg := &sync.WaitGroup{}
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return

			case <-stopCh:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	}()

	stopCh <- struct{}{}
	wg.Wait()
}

func TestMetricsIssue571(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2024.2.0",
		Path:    "configs/.rr-issue-571.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
		&server.Plugin{},
		&logger.Plugin{},
		&httpPlugin.Plugin{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	_, err = cont.Serve()
	assert.NoError(t, err)
	assert.NoError(t, cont.Stop())
}

func TestMetricsGaugeCollector(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2024.2.0",
		Path:    "configs/.rr-metrics-gauge.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
		&logger.Plugin{},
		&Plugin1{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	time.Sleep(time.Second)
	tt := time.NewTimer(time.Second * 5)
	defer tt.Stop()

	time.Sleep(time.Second * 2)
	out, err := getIPV6("http://[::1]:2113/metrics")
	assert.NoError(t, err)
	assert.Contains(t, out, "my_gauge 100")
	assert.Contains(t, out, "my_gauge2 100")

	out, err = getIPV6("http://[::1]:2113/metrics")
	assert.NoError(t, err)
	assert.Contains(t, out, "go_gc_duration_seconds")

	wg := &sync.WaitGroup{}
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return

			case <-stopCh:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	}()

	stopCh <- struct{}{}
	wg.Wait()
}

func TestMetricsDifferentRPCCalls(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2024.2.0",
		Path:    "configs/.rr-metrics-different-rpc-calls.yaml",
	}

	l, oLogger := mocklogger.ZapTestLogger(zap.DebugLevel)
	err := cont.RegisterAll(
		cfg,
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
		l,
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return

			case <-stopCh:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	}()

	address := "127.0.0.1:6002"
	time.Sleep(time.Second * 2)
	t.Run("DeclareMetric", declareMetricsTest(address))
	genericOut, err := getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "test_metrics_named_collector")

	t.Run("AddMetric", addMetricsTest(address))
	genericOut, err = getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "test_metrics_named_collector 10000")

	t.Run("SetMetric", setMetric(address))
	genericOut, err = getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "user_gauge_collector 100")

	t.Run("VectorMetric", vectorMetric(address))
	genericOut, err = getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "gauge_2_collector{section=\"first\",type=\"core\"} 100")

	t.Run("MissingSection", missingSection(address))
	t.Run("SetWithoutLabels", setWithoutLabels(address))
	t.Run("SetOnHistogram", setOnHistogram(address))
	t.Run("MetricSub", subMetric(address))
	genericOut, err = getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "sub_gauge_subMetric 1")

	t.Run("SubVector", subVector(address))
	genericOut, err = getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "sub_gauge_subVector{section=\"first\",type=\"core\"} 1")

	t.Run("RegisterHistogram", registerHistogram(address))

	genericOut, err = getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, `TYPE histogram_registerHistogram`)

	// check buckets
	assert.Contains(t, genericOut, `histogram_registerHistogram_bucket{le="0.1"} 0`)
	assert.Contains(t, genericOut, `histogram_registerHistogram_bucket{le="0.2"} 0`)
	assert.Contains(t, genericOut, `histogram_registerHistogram_bucket{le="0.5"} 0`)
	assert.Contains(t, genericOut, `histogram_registerHistogram_bucket{le="+Inf"} 0`)
	assert.Contains(t, genericOut, `histogram_registerHistogram_sum 0`)
	assert.Contains(t, genericOut, `histogram_registerHistogram_count 0`)

	t.Run("CounterMetric", counterMetric(address))
	genericOut, err = getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "HELP default_default_counter_CounterMetric test_counter")
	assert.Contains(t, genericOut, `default_default_counter_CounterMetric{section="section2",type="type2"}`)

	t.Run("ObserveMetric", observeMetric(address))
	genericOut, err = getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "observe_observeMetric")

	t.Run("ObserveMetricNotEnoughLabels", observeMetricNotEnoughLabels(address))

	t.Run("ConfiguredCounterMetric", configuredCounterMetric(address))
	genericOut, err = getIPV6("http://[::1]:2114/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "HELP app_metric_counter Custom application counter.")
	assert.Contains(t, genericOut, `app_metric_counter 100`)

	stopCh <- struct{}{}
	wg.Wait()

	require.Equal(t, 0, oLogger.FilterMessageSnippet("http server was started").Len())
	require.Equal(t, 0, oLogger.FilterMessageSnippet("http log").Len())

	require.Equal(t, 6, oLogger.FilterMessageSnippet("adding metric").Len())
	require.Equal(t, 17, oLogger.FilterMessageSnippet("metric successfully added").Len())
	require.Equal(t, 12, oLogger.FilterMessageSnippet("declaring new metric").Len())
	require.Equal(t, 7, oLogger.FilterMessageSnippet("observing metric").Len())
	require.Equal(t, 1, oLogger.FilterMessageSnippet("observe operation finished successfully").Len())

	require.Equal(t, 2, oLogger.FilterMessageSnippet("set operation finished successfully").Len())
	require.Equal(t, 2, oLogger.FilterMessageSnippet("subtracting value from metric").Len())
	require.Equal(t, 2, oLogger.FilterMessageSnippet("subtracting operation finished successfully").Len())
	require.Equal(t, 2, oLogger.FilterMessageSnippet("failed to get metrics with label values").Len())
	require.Equal(t, 1, oLogger.FilterMessageSnippet("required labels for collector").Len())
	require.Equal(t, 2, oLogger.FilterMessageSnippet("failed to get metrics with label values").Len())
}

func TestHTTPMetrics(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2024.2.0",
		Path:    "configs/.rr-metrics-http.yaml",
	}

	l, oLogger := mocklogger.ZapTestLogger(zap.DebugLevel)
	err := cont.RegisterAll(
		cfg,
		&metrics.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		l,
		&prometheus.Plugin{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	tt := time.NewTimer(time.Minute * 3)
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer tt.Stop()
		defer wg.Done()
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			case <-tt.C:
				// timeout
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	}()

	time.Sleep(time.Second * 2)
	t.Run("req1", echoHTTP("13223"))
	t.Run("req2", echoHTTP("13223"))

	time.Sleep(time.Second)
	genericOut, err := get("http://127.0.0.1:2115/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, `rr_http_request_duration_seconds_bucket`)
	assert.Contains(t, genericOut, `rr_http_request_duration_seconds_sum{status="200"}`)
	assert.Contains(t, genericOut, `rr_http_request_duration_seconds_count{status="200"}`)
	assert.Contains(t, genericOut, `rr_http_request_total{status="200"}`)
	assert.Contains(t, genericOut, "rr_http_workers_memory_bytes")
	assert.Contains(t, genericOut, `state="ready"}`)
	assert.Contains(t, genericOut, `{pid=`)
	assert.Contains(t, genericOut, `rr_http_total_workers 10`)

	close(sig)
	wg.Wait()

	require.Equal(t, 2, oLogger.FilterMessageSnippet("http log").Len())
}

func TestHTTPMetricsNoFreeWorkers(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2024.2.0",
		Path:    "configs/.rr-http-metrics-no-free-workers.yaml",
	}

	err := cont.RegisterAll(
		cfg,
		&metrics.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
		&logger.Plugin{},
		&prometheus.Plugin{},
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	tt := time.NewTimer(time.Minute * 3)

	go func() {
		defer tt.Stop()
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			case <-tt.C:
				// timeout
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	}()

	time.Sleep(time.Second * 2)
	go func() {
		t.Run("req_slow", echoHTTP("15442"))
	}()
	time.Sleep(time.Second * 2)
	t.Run("req2", echoHTTP("15442"))

	genericOut, err := get("http://127.0.0.1:2116/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, `rr_http_requests_queue`)
	assert.Contains(t, genericOut, `rr_http_no_free_workers_total 1`)

	close(sig)
}

func TestUnregister(t *testing.T) {
	cont := endure.New(slog.LevelDebug)

	cfg := &config.Plugin{
		Version: "2024.2.0",
		Path:    "configs/.rr-metrics-unregister.yaml",
	}

	l, oLogger := mocklogger.ZapTestLogger(zap.DebugLevel)
	err := cont.RegisterAll(
		cfg,
		&metrics.Plugin{},
		&rpcPlugin.Plugin{},
		l,
	)
	assert.NoError(t, err)

	err = cont.Init()
	if err != nil {
		t.Fatal(err)
	}

	ch, err := cont.Serve()
	assert.NoError(t, err)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}
	wg.Add(1)

	stopCh := make(chan struct{}, 1)

	go func() {
		defer wg.Done()
		for {
			select {
			case e := <-ch:
				assert.Fail(t, "error", e.Error.Error())
			case <-sig:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return

			case <-stopCh:
				err = cont.Stop()
				if err != nil {
					assert.FailNow(t, "error", err.Error())
				}
				return
			}
		}
	}()

	address := "127.0.0.1:6005"
	time.Sleep(time.Second * 2)
	t.Run("DeclareMetric", declareMetricsTest(address))
	genericOut, err := getIPV6("http://[::1]:2117/metrics")
	assert.NoError(t, err)
	assert.Contains(t, genericOut, "test_metrics_named_collector")

	time.Sleep(time.Second * 2)
	t.Run("UnregisterMetric", unregisterMetric("test_metrics_named_collector", address))
	genericOut, err = getIPV6("http://[::1]:2117/metrics")
	assert.NoError(t, err)
	assert.NotContains(t, genericOut, "test_metrics_named_collector")

	require.Equal(t, 1, oLogger.FilterMessageSnippet("collector was successfully unregistered").Len())

	stopCh <- struct{}{}
	wg.Wait()
}

func configuredCounterMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		assert.NoError(t, client.Call("metrics.Add", metrics.Metric{
			Name:  "app_metric_counter",
			Value: 100.0,
		}, &ret))
		assert.True(t, ret)
	}
}

func observeMetricNotEnoughLabels(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

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

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
		ret = false

		assert.Error(t, client.Call("metrics.Observe", metrics.Metric{
			Name:   "observe_observeMetric",
			Value:  100.0,
			Labels: []string{"test"},
		}, &ret))
		assert.False(t, ret)
	}
}

func observeMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

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

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
		ret = false

		assert.NoError(t, client.Call("metrics.Observe", metrics.Metric{
			Name:   "observe_observeMetric",
			Value:  100.0,
			Labels: []string{"test", "test2"},
		}, &ret))
		assert.True(t, ret)
	}
}

func counterMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

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

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)

		ret = false

		assert.NoError(t, client.Call("metrics.Add", metrics.Metric{
			Name:   "counter_CounterMetric",
			Value:  100.0,
			Labels: []string{"type2", "section2"},
		}, &ret))
		assert.True(t, ret)
	}
}

func registerHistogram(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		nc := metrics.NamedCollector{
			Name: "histogram_registerHistogram",
			Collector: metrics.Collector{
				Help:    "test_histogram",
				Type:    metrics.Histogram,
				Buckets: []float64{0.1, 0.2, 0.5},
			},
		}

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)

		ret = false

		m := metrics.Metric{
			Name:   "histogram_registerHistogram",
			Value:  10000,
			Labels: nil,
		}

		err = client.Call("metrics.Add", m, &ret)
		assert.Error(t, err)
		assert.False(t, ret)
	}
}

func subVector(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()

		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		nc := metrics.NamedCollector{
			Name: "sub_gauge_subVector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
				Labels:    []string{"type", "section"},
			},
		}

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
		ret = false

		m := metrics.Metric{
			Name:   "sub_gauge_subVector",
			Value:  100000,
			Labels: []string{"core", "first"},
		}

		err = client.Call("metrics.Add", m, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
		ret = false

		m = metrics.Metric{
			Name:   "sub_gauge_subVector",
			Value:  99999,
			Labels: []string{"core", "first"},
		}

		err = client.Call("metrics.Sub", m, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
	}
}

func subMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		nc := metrics.NamedCollector{
			Name: "sub_gauge_subMetric",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
			},
		}

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
		ret = false

		m := metrics.Metric{
			Name:  "sub_gauge_subMetric",
			Value: 100000,
		}

		err = client.Call("metrics.Add", m, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
		ret = false

		m = metrics.Metric{
			Name:  "sub_gauge_subMetric",
			Value: 99999,
		}

		err = client.Call("metrics.Sub", m, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
	}
}

func setOnHistogram(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		nc := metrics.NamedCollector{
			Name: "histogram_setOnHistogram",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Histogram,
				Labels:    []string{"type", "section"},
			},
		}

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)

		ret = false

		m := metrics.Metric{
			Name:  "gauge_setOnHistogram",
			Value: 100.0,
		}

		err = client.Call("metrics.Set", m, &ret) // expected 2 label values but got 1 in []string{"missing"}
		assert.Error(t, err)
		assert.False(t, ret)
	}
}

func setWithoutLabels(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		nc := metrics.NamedCollector{
			Name: "gauge_setWithoutLabels",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
				Labels:    []string{"type", "section"},
			},
		}

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)

		ret = false

		m := metrics.Metric{
			Name:  "gauge_setWithoutLabels",
			Value: 100.0,
		}

		err = client.Call("metrics.Set", m, &ret) // expected 2 label values but got 1 in []string{"missing"}
		assert.Error(t, err)
		assert.False(t, ret)
	}
}

func missingSection(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		nc := metrics.NamedCollector{
			Name: "gauge_missing_section_collector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
				Labels:    []string{"type", "section"},
			},
		}

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)

		ret = false

		m := metrics.Metric{
			Name:   "gauge_missing_section_collector",
			Value:  100.0,
			Labels: []string{"missing"},
		}

		err = client.Call("metrics.Set", m, &ret) // expected 2 label values but got 1 in []string{"missing"}
		assert.Error(t, err)
		assert.False(t, ret)
	}
}

func vectorMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		nc := metrics.NamedCollector{
			Name: "gauge_2_collector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
				Labels:    []string{"type", "section"},
			},
		}

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)

		ret = false

		m := metrics.Metric{
			Name:   "gauge_2_collector",
			Value:  100.0,
			Labels: []string{"core", "first"},
		}

		err = client.Call("metrics.Set", m, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
	}
}

func setMetric(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		nc := metrics.NamedCollector{
			Name: "user_gauge_collector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Gauge,
			},
		}

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
		ret = false

		m := metrics.Metric{
			Name:  "user_gauge_collector",
			Value: 100.0,
		}

		err = client.Call("metrics.Set", m, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
	}
}

func addMetricsTest(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		m := metrics.Metric{
			Name:   "test_metrics_named_collector",
			Value:  10000,
			Labels: nil,
		}

		err = client.Call("metrics.Add", m, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
	}
}

func declareMetricsTest(address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)

		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		nc := metrics.NamedCollector{
			Name: "test_metrics_named_collector",
			Collector: metrics.Collector{
				Namespace: "default",
				Subsystem: "default",
				Type:      metrics.Counter,
				Help:      "NO HELP!",
				Labels:    nil,
				Buckets:   nil,
			},
		}

		err = client.Call("metrics.Declare", nc, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
	}
}

func unregisterMetric(name string, address string) func(t *testing.T) {
	return func(t *testing.T) {
		conn, err := net.Dial(dialNetwork, address)
		assert.NoError(t, err)
		defer func() {
			_ = conn.Close()
		}()
		client := rpc.NewClientWithCodec(goridgeRpc.NewClientCodec(conn))
		var ret bool

		err = client.Call("metrics.Unregister", name, &ret)
		assert.NoError(t, err)
		assert.True(t, ret)
	}
}

func echoHTTP(port string) func(t *testing.T) {
	return func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://127.0.0.1:"+port, nil)
		assert.NoError(t, err)

		r, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_, err = io.ReadAll(r.Body)
		assert.NoError(t, err)

		err = r.Body.Close()
		assert.NoError(t, err)
	}
}

// get request and return body
func get(address string) (string, error) {
	r, err := http.Get(address) //nolint:gosec
	if err != nil {
		return "", err
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}

	err = r.Body.Close()
	if err != nil {
		return "", err
	}
	// unsafe
	return string(b), err
}

// get request and return body
func getIPV6(address string) (string, error) {
	r, err := http.Get(address) //nolint:gosec
	if err != nil {
		return "", err
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}

	err = r.Body.Close()
	if err != nil {
		return "", err
	}
	// unsafe
	return string(b), err
}
