package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigCollectorTypes(t *testing.T) {
	tests := []struct {
		name       string
		collector  Collector
		wantScalar prometheus.Collector
		wantVec    prometheus.Collector
	}{
		{
			name:       "gauge",
			collector:  Collector{Type: Gauge},
			wantScalar: prometheus.NewGauge(prometheus.GaugeOpts{}),
			wantVec:    prometheus.NewGaugeVec(prometheus.GaugeOpts{}, []string{}),
		},
		{
			name:       "counter",
			collector:  Collector{Type: Counter},
			wantScalar: prometheus.NewCounter(prometheus.CounterOpts{}),
			wantVec:    prometheus.NewCounterVec(prometheus.CounterOpts{}, []string{}),
		},
		{
			name:       "summary",
			collector:  Collector{Type: Summary, Objectives: map[float64]float64{0.5: 0.05}},
			wantScalar: prometheus.NewSummary(prometheus.SummaryOpts{Objectives: map[float64]float64{0.5: 0.05}}),
			wantVec:    prometheus.NewSummaryVec(prometheus.SummaryOpts{}, []string{}),
		},
		{
			name:       "histogram",
			collector:  Collector{Type: Histogram, Buckets: []float64{0.1, 0.2}},
			wantScalar: prometheus.NewHistogram(prometheus.HistogramOpts{}),
			wantVec:    prometheus.NewHistogramVec(prometheus.HistogramOpts{}, []string{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scalar := tt.collector
			scalar.Namespace = "app"
			scalar.Subsystem = "sub"
			scalar.Help = "help"

			cfg := &Config{Collect: map[string]Collector{"metric": scalar}}
			built, err := cfg.getCollectors()
			require.NoError(t, err)
			assert.IsType(t, tt.wantScalar, built["metric"].col)
			assert.False(t, built["metric"].registered)

			vec := scalar
			vec.Labels = []string{"label"}

			cfg = &Config{Collect: map[string]Collector{"metric": vec}}
			built, err = cfg.getCollectors()
			require.NoError(t, err)
			assert.IsType(t, tt.wantVec, built["metric"].col)
		})
	}
}

func TestConfigCollectorUnknownType(t *testing.T) {
	cfg := &Config{Collect: map[string]Collector{"metric": {Type: "gaugee"}}}

	built, err := cfg.getCollectors()
	require.Error(t, err)
	assert.Nil(t, built)
	assert.EqualError(t, err, "invalid metric type `gaugee` for `metric`")
}

func TestConfigNoCollectors(t *testing.T) {
	cfg := &Config{}

	built, err := cfg.getCollectors()
	require.NoError(t, err)
	assert.Nil(t, built)
}

func TestConfigInitDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.InitDefaults()
	assert.Equal(t, "127.0.0.1:2112", cfg.Address)

	configured := &Config{Address: "127.0.0.1:9999"}
	configured.InitDefaults()
	assert.Equal(t, "127.0.0.1:9999", configured.Address)
}
