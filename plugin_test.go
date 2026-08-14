package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	rrerrors "github.com/roadrunner-server/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staticConfigurer hands Init a config value without going through viper.
type staticConfigurer struct {
	section string
	cfg     *Config
	err     error
}

func (s *staticConfigurer) Has(name string) bool { return name == s.section }

func (s *staticConfigurer) UnmarshalKey(_ string, out any) error {
	if s.err != nil {
		return s.err
	}

	dst, ok := out.(**Config)
	if !ok {
		return errors.New("unexpected unmarshal destination")
	}

	*dst = s.cfg

	return nil
}

// discardLogger provides a logger that drops every record.
type discardLogger struct{}

func (discardLogger) NamedLogger(string) *slog.Logger { return slog.New(slog.DiscardHandler) }

// gaugeProvider is a StatProvider handing out a single named gauge.
type gaugeProvider struct{ name string }

func (g *gaugeProvider) MetricsCollector() []prometheus.Collector {
	return []prometheus.Collector{prometheus.NewGauge(prometheus.GaugeOpts{Name: g.name})}
}

// newTestPlugin returns a plugin with the state Init would have produced, minus
// the config, so that tests can drive Serve and Stop directly.
func newTestPlugin() *Plugin {
	return &Plugin{
		log:      slog.New(slog.DiscardHandler),
		registry: prometheus.NewRegistry(),
	}
}

// gatheredNames returns the names of the metric families the registry holds.
func gatheredNames(t *testing.T, reg *prometheus.Registry) []string {
	t.Helper()

	families, err := reg.Gather()
	require.NoError(t, err)

	names := make([]string, 0, len(families))
	for _, f := range families {
		names = append(names, f.GetName())
	}

	return names
}

func TestPluginInitWithoutSection(t *testing.T) {
	p := &Plugin{}

	err := p.Init(&staticConfigurer{}, discardLogger{})
	require.Error(t, err)
	assert.True(t, rrerrors.Is(rrerrors.Disabled, err))
}

func TestPluginInitUnmarshalFailure(t *testing.T) {
	p := &Plugin{}

	err := p.Init(&staticConfigurer{section: PluginName, err: errors.New("broken section")}, discardLogger{})
	require.ErrorContains(t, err, "broken section")
	assert.True(t, rrerrors.Is(rrerrors.Disabled, err))
}

func TestPluginInitUnknownCollectorType(t *testing.T) {
	p := &Plugin{}

	err := p.Init(&staticConfigurer{
		section: PluginName,
		cfg:     &Config{Collect: map[string]Collector{"metric": {Type: "gaugee"}}},
	}, discardLogger{})
	require.ErrorContains(t, err, "invalid metric type `gaugee` for `metric`")
}

func TestPluginInit(t *testing.T) {
	p := &Plugin{}

	require.NoError(t, p.Init(&staticConfigurer{
		section: PluginName,
		cfg:     &Config{Collect: map[string]Collector{"configured": {Type: Counter}}},
	}, discardLogger{}))

	assert.Equal(t, "127.0.0.1:2112", p.cfg.Address)
	assert.Equal(t, PluginName, p.Name())
	assert.Equal(t, uint(1), p.Weight())
	assert.Len(t, p.Collects(), 1)
	assert.IsType(t, &rpc{}, p.RPC())

	// configured collectors are stored unregistered, Serve registers them
	stored, ok := p.collectors.Load("configured")
	require.True(t, ok)
	assert.False(t, stored.(*collector).registered)

	// the process and go collectors are registered by Init itself
	assert.Contains(t, gatheredNames(t, p.registry), "go_goroutines")
}

func TestPluginRegisterRejectsDuplicate(t *testing.T) {
	p := newTestPlugin()

	require.NoError(t, p.Register(prometheus.NewCounter(prometheus.CounterOpts{Name: "dup"})))
	require.Error(t, p.Register(prometheus.NewCounter(prometheus.CounterOpts{Name: "dup"})))
}

func TestPluginServe(t *testing.T) {
	p := newTestPlugin()
	p.cfg = &Config{Address: "127.0.0.1:0"}
	p.statProviders = []StatProvider{&gaugeProvider{name: "provided"}}
	p.collectors.Store("configured", &collector{col: prometheus.NewCounter(prometheus.CounterOpts{Name: "configured"})})
	p.collectors.Store("skipped", &collector{col: prometheus.NewCounter(prometheus.CounterOpts{Name: "skipped"}), registered: true})

	errCh := p.Serve()

	names := gatheredNames(t, p.registry)
	assert.Contains(t, names, "provided")
	assert.Contains(t, names, "configured")
	// the collector claims to be registered already, so Serve leaves it alone
	assert.NotContains(t, names, "skipped")

	require.NoError(t, p.Stop(t.Context()))
	assert.Empty(t, errCh)
}

func TestPluginServeStatProviderConflict(t *testing.T) {
	p := newTestPlugin()
	p.cfg = &Config{Address: "127.0.0.1:0"}
	require.NoError(t, p.Register(prometheus.NewGauge(prometheus.GaugeOpts{Name: "provided"})))
	p.statProviders = []StatProvider{&gaugeProvider{name: "provided"}}

	err := <-p.Serve()
	require.Error(t, err)

	// the http server is never reached on this path
	assert.Nil(t, p.http)
	require.NoError(t, p.Stop(t.Context()))
}

func TestPluginServeConfiguredCollectorConflict(t *testing.T) {
	p := newTestPlugin()
	p.cfg = &Config{Address: "127.0.0.1:0"}
	require.NoError(t, p.Register(prometheus.NewCounter(prometheus.CounterOpts{Name: "configured"})))
	p.collectors.Store("configured", &collector{col: prometheus.NewCounter(prometheus.CounterOpts{Name: "configured"})})

	err := <-p.Serve()
	require.Error(t, err)

	require.NoError(t, p.Stop(t.Context()))
}

func TestPluginServeAddressInUse(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer func() { _ = ln.Close() }()

	p := newTestPlugin()
	p.cfg = &Config{Address: ln.Addr().String()}

	require.Error(t, <-p.Serve())
	require.NoError(t, p.Stop(t.Context()))
}

func TestPluginStopWithoutServer(t *testing.T) {
	p := newTestPlugin()

	require.NoError(t, p.Stop(t.Context()))
}

func TestPluginStopReportsShutdownFailure(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// the hook fires once the server has taken the connection into its active
	// set, which is what keeps Shutdown from returning right away
	tracked := make(chan struct{}, 1)

	p := newTestPlugin()
	p.http = &http.Server{
		Handler:           http.NewServeMux(),
		ReadHeaderTimeout: time.Second,
		ConnState: func(_ net.Conn, state http.ConnState) {
			if state != http.StateNew {
				return
			}

			select {
			case tracked <- struct{}{}:
			default:
			}
		},
	}

	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = p.http.Serve(ln)
	}()

	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "tcp", ln.Addr().String())
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	<-tracked

	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond*50)
	defer cancel()

	require.ErrorContains(t, p.Stop(ctx), "error shutting down the metrics server")
	<-served
}
