package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

// GaugeStatProvider is a plugin that feeds two constant gauges to the metrics
// plugin through the StatProvider interface.
type GaugeStatProvider struct{}

func (g *GaugeStatProvider) Init() error {
	return nil
}

func (g *GaugeStatProvider) Serve() chan error {
	errCh := make(chan error, 1)
	return errCh
}

func (g *GaugeStatProvider) Stop(context.Context) error {
	return nil
}

func (g *GaugeStatProvider) Name() string {
	return "metrics_test.gauge_stat_provider"
}

func (g *GaugeStatProvider) MetricsCollector() []prometheus.Collector {
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "my_gauge",
		Help: "My gauge value",
	})

	gauge.Set(100)

	gauge2 := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "my_gauge2",
		Help: "My gauge2 value",
	})

	gauge2.Set(100)

	return []prometheus.Collector{gauge, gauge2}
}
