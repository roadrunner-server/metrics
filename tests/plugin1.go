package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

type Configurer interface {
	// UnmarshalKey takes a single key and unmarshal it into a Struct.
	UnmarshalKey(name string, out any) error
	// Has checks if a config section exists.
	Has(name string) bool
}

// Gauge //////////////
type Plugin1 struct {
	config Configurer
}

func (p1 *Plugin1) Init(cfg Configurer) error {
	p1.config = cfg
	return nil
}

func (p1 *Plugin1) Serve() chan error {
	errCh := make(chan error, 1)
	return errCh
}

func (p1 *Plugin1) Stop(context.Context) error {
	return nil
}

func (p1 *Plugin1) Name() string {
	return "metrics_test.plugin1"
}

func (p1 *Plugin1) MetricsCollector() []prometheus.Collector {
	collector := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "my_gauge",
		Help: "My gauge value",
	})

	collector.Set(100)

	collector2 := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "my_gauge2",
		Help: "My gauge2 value",
	})

	collector2.Set(100)
	return []prometheus.Collector{collector, collector2}
}
