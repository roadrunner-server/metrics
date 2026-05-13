package metrics

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	metricsV1 "github.com/roadrunner-server/api-go/v6/metrics/v1"
	"github.com/roadrunner-server/api-go/v6/metrics/v1/metricsV1connect"
	"github.com/roadrunner-server/config/v6"
	"github.com/roadrunner-server/endure/v2"
	"github.com/roadrunner-server/logger/v6"
	"github.com/roadrunner-server/metrics/v6"
	rpcPlugin "github.com/roadrunner-server/rpc/v6"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const metricsAPIAddr = "127.0.0.1:6001"

// startMetricsAPIContainer brings up rpc + metrics + logger with one pre-
// configured counter (`app_metric_counter`) — the API tests exercise the
// wire surface against this collector and against runtime-declared ones.
func startMetricsAPIContainer(t *testing.T) func() {
	t.Helper()

	cont := endure.New(slog.LevelError)
	cfg := &config.Plugin{
		Version: "2024.2.0",
		Path:    "configs/.rr-metrics-api.yaml",
	}

	require.NoError(t, cont.RegisterAll(
		cfg,
		&logger.Plugin{},
		&rpcPlugin.Plugin{},
		&metrics.Plugin{},
	))
	require.NoError(t, cont.Init())

	ch, err := cont.Serve()
	require.NoError(t, err)

	wg := &sync.WaitGroup{}
	stop := make(chan struct{})
	wg.Go(func() {
		select {
		case e := <-ch:
			t.Errorf("container reported error: %v", e.Error)
		case <-stop:
		}
	})

	time.Sleep(500 * time.Millisecond)

	return func() {
		close(stop)
		require.NoError(t, cont.Stop())
		wg.Wait()
	}
}

func TestMetricsConnectAPI(t *testing.T) {
	stop := startMetricsAPIContainer(t)
	defer stop()

	httpc := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, network, addr)
			},
		},
	}
	client := metricsV1connect.NewMetricsServiceClient(httpc, "http://"+metricsAPIAddr)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Declare a new gauge, set it, then read it back via Add.
	declareResp, err := client.Declare(ctx, connect.NewRequest(&metricsV1.DeclareRequest{
		Collector: &metricsV1.NamedCollector{
			Name: "api_gauge",
			Collector: &metricsV1.Collector{
				Type: metricsV1.CollectorType_COLLECTOR_TYPE_GAUGE,
				Help: "API test gauge",
			},
		},
	}))
	require.NoError(t, err)
	require.True(t, declareResp.Msg.GetOk())

	setResp, err := client.Set(ctx, connect.NewRequest(&metricsV1.SetRequest{
		Metric: &metricsV1.Metric{Name: "api_gauge", Value: 42.0},
	}))
	require.NoError(t, err)
	require.True(t, setResp.Msg.GetOk())

	// Unknown collector → CodeNotFound.
	_, err = client.Add(ctx, connect.NewRequest(&metricsV1.AddRequest{
		Metric: &metricsV1.Metric{Name: "does-not-exist", Value: 1},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// Operation unsupported by collector kind → CodeFailedPrecondition.
	// Histogram doesn't support Set.
	_, err = client.Declare(ctx, connect.NewRequest(&metricsV1.DeclareRequest{
		Collector: &metricsV1.NamedCollector{
			Name: "api_histogram",
			Collector: &metricsV1.Collector{
				Type:    metricsV1.CollectorType_COLLECTOR_TYPE_HISTOGRAM,
				Help:    "API test histogram",
				Buckets: []float64{0.1, 1.0},
			},
		},
	}))
	require.NoError(t, err)
	_, err = client.Set(ctx, connect.NewRequest(&metricsV1.SetRequest{
		Metric: &metricsV1.Metric{Name: "api_histogram", Value: 0.5},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	// Unregister an existing collector → ok.
	unregResp, err := client.Unregister(ctx, connect.NewRequest(&metricsV1.UnregisterRequest{Name: "api_gauge"}))
	require.NoError(t, err)
	require.True(t, unregResp.Msg.GetOk())

	// Unregister a missing collector → CodeNotFound.
	_, err = client.Unregister(ctx, connect.NewRequest(&metricsV1.UnregisterRequest{Name: "missing"}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// TestMetricsHTTPApi exercises the metrics RPCs through plain HTTP/1.1 with
// a protojson body — the shape any non-Connect HTTP client uses against this
// handler.
func TestMetricsHTTPApi(t *testing.T) {
	stop := startMetricsAPIContainer(t)
	defer stop()

	httpc := &http.Client{Timeout: 30 * time.Second}
	ctx := t.Context()

	call := func(method string, in proto.Message, out proto.Message) {
		body, err := protojson.Marshal(in)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"http://"+metricsAPIAddr+"/metrics.v1.MetricsService/"+method, bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpc.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equalf(t, http.StatusOK, resp.StatusCode, "method=%s body=%s", method, respBody)
		require.NoError(t, protojson.Unmarshal(respBody, out))
	}

	var declareResp metricsV1.Response
	call("Declare", &metricsV1.DeclareRequest{
		Collector: &metricsV1.NamedCollector{
			Name: "http_counter",
			Collector: &metricsV1.Collector{
				Type: metricsV1.CollectorType_COLLECTOR_TYPE_COUNTER,
				Help: "HTTP API test counter",
			},
		},
	}, &declareResp)
	require.True(t, declareResp.GetOk())

	var addResp metricsV1.Response
	call("Add", &metricsV1.AddRequest{
		Metric: &metricsV1.Metric{Name: "http_counter", Value: 7.0},
	}, &addResp)
	require.True(t, addResp.GetOk())
}

// TestMetricsGRPCApi exercises the metrics RPCs through a regular gRPC client.
// The same Connect handler serves gRPC framing off the same port.
func TestMetricsGRPCApi(t *testing.T) {
	stop := startMetricsAPIContainer(t)
	defer stop()

	conn, err := grpc.NewClient(metricsAPIAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := metricsV1.NewMetricsServiceClient(conn)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	declareResp, err := client.Declare(ctx, &metricsV1.DeclareRequest{
		Collector: &metricsV1.NamedCollector{
			Name: "grpc_counter",
			Collector: &metricsV1.Collector{
				Type: metricsV1.CollectorType_COLLECTOR_TYPE_COUNTER,
				Help: "gRPC API test counter",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, declareResp.GetOk())

	addResp, err := client.Add(ctx, &metricsV1.AddRequest{
		Metric: &metricsV1.Metric{Name: "grpc_counter", Value: 3.0},
	})
	require.NoError(t, err)
	require.True(t, addResp.GetOk())
}

// TestMetricsHTTPGetIdempotency verifies that all six metrics RPCs reject
// HTTP GET — none of them are marked NO_SIDE_EFFECTS in the proto (every
// method mutates Prometheus state in some way).
func TestMetricsHTTPGetIdempotency(t *testing.T) {
	stop := startMetricsAPIContainer(t)
	defer stop()

	body, err := protojson.Marshal(&metricsV1.AddRequest{Metric: &metricsV1.Metric{Name: "probe"}})
	require.NoError(t, err)

	q := url.Values{}
	q.Set("encoding", "json")
	q.Set("base64", "1")
	q.Set("message", base64.URLEncoding.EncodeToString(body))

	methods := []string{"Add", "Sub", "Observe", "Set", "Declare", "Unregister"}

	httpc := &http.Client{Timeout: 30 * time.Second}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
				"http://"+metricsAPIAddr+"/metrics.v1.MetricsService/"+m+"?"+q.Encode(), nil)
			require.NoError(t, err)

			resp, err := httpc.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			require.Equalf(t, http.StatusMethodNotAllowed, resp.StatusCode,
				"%s via GET should be rejected; got %s", m, resp.Status)
		})
	}
}
