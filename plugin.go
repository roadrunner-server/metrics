package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/roadrunner-server/api-go/v6/metrics/v1/metricsV1connect"
	"github.com/roadrunner-server/endure/v2/dep"
	rrerrors "github.com/roadrunner-server/errors"
)

const (
	// PluginName declares plugin name.
	PluginName = "metrics"
	// maxHeaderSize declares max header size for prometheus server
	maxHeaderSize = 1 << 20 // 1MB
)

// Plugin to manage application metrics using Prometheus.
type Plugin struct {
	cfg        *Config
	log        *slog.Logger
	mu         sync.Mutex // all receivers are pointers
	http       *http.Server
	collectors sync.Map // name -> collector
	registry   *prometheus.Registry

	// prometheus Collectors
	statProviders []StatProvider
}

// collector used to deduplicate registration
type collector struct {
	col        prometheus.Collector
	registered bool
}

type Configurer interface {
	// UnmarshalKey takes a single key and unmarshal it into a Struct.
	UnmarshalKey(name string, out any) error
	// Has checks if config section exists.
	Has(name string) bool
}

type Logger interface {
	NamedLogger(name string) *slog.Logger
}

// StatProvider used to collect all plugins which might report to the prometheus
type StatProvider interface {
	MetricsCollector() []prometheus.Collector
}

// Init service.
func (p *Plugin) Init(cfg Configurer, log Logger) error {
	const op = rrerrors.Op("metrics_plugin_init")
	if !cfg.Has(PluginName) {
		return rrerrors.E(op, rrerrors.Disabled)
	}

	err := cfg.UnmarshalKey(PluginName, &p.cfg)
	if err != nil {
		return rrerrors.E(op, rrerrors.Disabled, err)
	}

	p.cfg.InitDefaults()

	p.log = log.NamedLogger(PluginName)
	p.registry = prometheus.NewRegistry()

	// Default
	err = p.registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	if err != nil {
		return rrerrors.E(op, err)
	}

	// Default
	err = p.registry.Register(collectors.NewGoCollector())
	if err != nil {
		return rrerrors.E(op, err)
	}

	cl, err := p.cfg.getCollectors()
	if err != nil {
		return rrerrors.E(op, err)
	}

	// Register invocation will be later in the Serve method
	for k, v := range cl {
		p.collectors.Store(k, v)
	}

	return nil
}

// Register new prometheus collector.
func (p *Plugin) Register(c prometheus.Collector) error {
	return p.registry.Register(c)
}

// Serve prometheus metrics service.
func (p *Plugin) Serve() chan error {
	errCh := make(chan error, 1)
	p.mu.Lock()
	defer p.mu.Unlock()

	// register Collected stat providers
	for _, sp := range p.statProviders {
		for _, c := range sp.MetricsCollector() {
			err := p.registry.Register(c)
			if err != nil {
				errCh <- err
				return errCh
			}
		}
	}

	// range over the collectors registered via configuration
	p.collectors.Range(func(_, value any) bool {
		// key - name
		// value - prometheus.Collector
		c := value.(*collector)
		// do not register yet registered collectors
		if c.registered {
			p.log.Debug("prometheus collector was already registered, skipping")
			return true
		}

		if err := p.registry.Register(c.col); err != nil {
			errCh <- err
			return false
		}

		return true
	})

	p.http = &http.Server{
		Addr:              p.cfg.Address,
		Handler:           promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{}),
		IdleTimeout:       time.Hour,
		ReadTimeout:       time.Minute * 2,
		MaxHeaderBytes:    maxHeaderSize,
		ReadHeaderTimeout: time.Minute * 2,
		WriteTimeout:      time.Minute * 2,
	}

	go func() {
		err := p.http.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	return errCh
}

func (p *Plugin) Weight() uint {
	return 1
}

// Stop prometheus metrics service.
func (p *Plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.http != nil {
		err := p.http.Shutdown(ctx)
		if err != nil {
			return rrerrors.Errorf("error shutting down the metrics server: error %v", err)
		}
	}
	return nil
}

// Collects used to collect all plugins that implement metrics.StatProvider interface (and Named)
func (p *Plugin) Collects() []*dep.In {
	return []*dep.In{
		dep.Fits(func(pp any) {
			sp := pp.(StatProvider)
			p.statProviders = append(p.statProviders, sp)
		}, (*StatProvider)(nil)),
	}
}

// Name returns user-friendly plugin name
func (p *Plugin) Name() string {
	return PluginName
}

// RPC returns the Connect-RPC handler mount for the metrics service.
func (p *Plugin) RPC() (string, http.Handler) {
	return metricsV1connect.NewMetricsServiceHandler(&rpc{p: p, log: p.log})
}
