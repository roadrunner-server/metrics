package metrics

import (
	"context"
	"crypto/tls"
	stderr "errors"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/roadrunner-server/endure/v2/dep"
	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"
	"golang.org/x/sys/cpu"
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
	log        *zap.Logger
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
	// Has checks if a config section exists.
	Has(name string) bool
}

type Logger interface {
	NamedLogger(name string) *zap.Logger
}

// StatProvider used to collect all plugins that might report to the prometheus
type StatProvider interface {
	MetricsCollector() []prometheus.Collector
}

// Init service.
func (p *Plugin) Init(cfg Configurer, log Logger) error {
	const op = errors.Op("metrics_plugin_init")
	if !cfg.Has(PluginName) {
		return errors.E(op, errors.Disabled)
	}

	err := cfg.UnmarshalKey(PluginName, &p.cfg)
	if err != nil {
		return errors.E(op, errors.Disabled, err)
	}

	p.cfg.InitDefaults()

	p.log = log.NamedLogger(PluginName)
	p.registry = prometheus.NewRegistry()

	// Default
	err = p.registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	if err != nil {
		return errors.E(op, err)
	}

	// Default
	err = p.registry.Register(collectors.NewGoCollector())
	if err != nil {
		return errors.E(op, err)
	}

	cl, err := p.cfg.getCollectors()
	if err != nil {
		return errors.E(op, err)
	}

	// Register invocation will be later in the Serve method
	for k, v := range cl {
		p.collectors.Store(k, v)
	}

	p.statProviders = make([]StatProvider, 0, 2)

	return nil
}

// Register new prometheus collector.
func (p *Plugin) Register(c prometheus.Collector) error {
	return p.registry.Register(c)
}

// Serve prometheus metrics service.
func (p *Plugin) Serve() chan error { //nolint:gocyclo
	errCh := make(chan error, 1)
	p.mu.Lock()
	defer p.mu.Unlock()

	// register Collected stat providers
	for i := 0; i < len(p.statProviders); i++ {
		sp := p.statProviders[i]
		for _, c := range sp.MetricsCollector() {
			err := p.registry.Register(c)
			if err != nil {
				errCh <- err
				return errCh
			}
		}
	}

	// range over the collectors registered via configuration
	p.collectors.Range(func(key, value any) bool {
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

	var topCipherSuites []uint16
	var defaultCipherSuitesTLS13 []uint16

	hasGCMAsmAMD64 := cpu.X86.HasAES && cpu.X86.HasPCLMULQDQ
	hasGCMAsmARM64 := cpu.ARM64.HasAES && cpu.ARM64.HasPMULL
	// Keep in sync with crypto/aes/cipher_s390x.go.
	hasGCMAsmS390X := cpu.S390X.HasAES && cpu.S390X.HasAESCBC && cpu.S390X.HasAESCTR && (cpu.S390X.HasGHASH || cpu.S390X.HasAESGCM)

	hasGCMAsm := hasGCMAsmAMD64 || hasGCMAsmARM64 || hasGCMAsmS390X

	if hasGCMAsm {
		// If AES-GCM hardware is provided then prioritize AES-GCM
		// cipher suites.
		topCipherSuites = []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		}
		defaultCipherSuitesTLS13 = []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
		}
	} else {
		// Without AES-GCM hardware, we put the ChaCha20-Poly1305
		// cipher suites first.
		topCipherSuites = []uint16{
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		}
		defaultCipherSuitesTLS13 = []uint16{
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
		}
	}

	DefaultCipherSuites := make([]uint16, 0, 22)
	DefaultCipherSuites = append(DefaultCipherSuites, topCipherSuites...)
	DefaultCipherSuites = append(DefaultCipherSuites, defaultCipherSuitesTLS13...)

	p.http = &http.Server{
		Addr:              p.cfg.Address,
		Handler:           promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{}),
		IdleTimeout:       time.Hour,
		ReadTimeout:       time.Minute * 2,
		MaxHeaderBytes:    maxHeaderSize,
		ReadHeaderTimeout: time.Minute * 2,
		WriteTimeout:      time.Minute * 2,
		TLSConfig: &tls.Config{
			CurvePreferences: []tls.CurveID{
				tls.X25519,
				tls.CurveP256,
				tls.CurveP384,
				tls.CurveP521,
			},
			CipherSuites: DefaultCipherSuites,
			MinVersion:   tls.VersionTLS12,
		},
	}

	go func() {
		err := p.http.ListenAndServe()
		if err != nil && !stderr.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
	}()

	return errCh
}

func (p *Plugin) Weight() uint {
	return 1
}

// Stop prometheus metrics service.
func (p *Plugin) Stop(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.http != nil {
		// timeout is 10 seconds
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		err := p.http.Shutdown(ctx)
		if err != nil {
			// Function should be Stop() error
			p.log.Error("stop error", zap.Error(errors.Errorf("error shutting down the metrics server: error %v", err)))
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

// RPC interface satisfaction
func (p *Plugin) RPC() any {
	return &rpc{
		p:   p,
		log: p.log,
	}
}

func collectorKey(name, namespace string) string {
	return name + "@" + namespace
}
