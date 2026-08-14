package metrics

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRPC returns the rpc service backed by an otherwise empty plugin.
func newTestRPC() *rpc {
	p := newTestPlugin()
	return &rpc{p: p, log: p.log}
}

// declare registers nc through the rpc service and requires it to succeed.
func declare(t *testing.T, r *rpc, nc NamedCollector) {
	t.Helper()

	var ok bool
	require.NoError(t, r.Declare(&nc, &ok))
	require.True(t, ok)
}

// collectedValue returns the single sample of a scalar counter or gauge family.
func collectedValue(t *testing.T, r *rpc, name string) float64 {
	t.Helper()

	families, err := r.p.registry.Gather()
	require.NoError(t, err)

	for _, f := range families {
		if f.GetName() != name {
			continue
		}

		require.Len(t, f.GetMetric(), 1)
		m := f.GetMetric()[0]

		switch {
		case m.Counter != nil:
			return m.GetCounter().GetValue()
		case m.Gauge != nil:
			return m.GetGauge().GetValue()
		}

		require.FailNow(t, "metric is neither a counter nor a gauge", name)
	}

	require.FailNow(t, "metric was not collected", name)

	return 0
}

// collectedSampleCount returns the sample count of a scalar histogram or summary.
func collectedSampleCount(t *testing.T, r *rpc, name string) uint64 {
	t.Helper()

	families, err := r.p.registry.Gather()
	require.NoError(t, err)

	for _, f := range families {
		if f.GetName() != name {
			continue
		}

		require.Len(t, f.GetMetric(), 1)
		m := f.GetMetric()[0]

		switch {
		case m.Histogram != nil:
			return m.GetHistogram().GetSampleCount()
		case m.Summary != nil:
			return m.GetSummary().GetSampleCount()
		}

		require.FailNow(t, "metric is neither a histogram nor a summary", name)
	}

	require.FailNow(t, "metric was not collected", name)

	return 0
}

func TestRPCDeclareCollectorTypes(t *testing.T) {
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
			name:       "histogram",
			collector:  Collector{Type: Histogram, Buckets: []float64{0.1, 1}},
			wantScalar: prometheus.NewHistogram(prometheus.HistogramOpts{}),
			wantVec:    prometheus.NewHistogramVec(prometheus.HistogramOpts{}, []string{}),
		},
		{
			name:       "summary",
			collector:  Collector{Type: Summary, Objectives: map[float64]float64{0.5: 0.05}},
			wantScalar: prometheus.NewSummary(prometheus.SummaryOpts{Objectives: map[float64]float64{0.5: 0.05}}),
			wantVec:    prometheus.NewSummaryVec(prometheus.SummaryOpts{}, []string{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRPC()

			scalar := tt.collector
			scalar.Help = "help"
			declare(t, r, NamedCollector{Name: "scalar", Collector: scalar})

			vec := scalar
			vec.Labels = []string{"label"}
			declare(t, r, NamedCollector{Name: "vec", Collector: vec})

			stored, ok := r.p.collectors.Load("scalar")
			require.True(t, ok)
			assert.IsType(t, tt.wantScalar, stored.(*collector).col)
			assert.True(t, stored.(*collector).registered)

			stored, ok = r.p.collectors.Load("vec")
			require.True(t, ok)
			assert.IsType(t, tt.wantVec, stored.(*collector).col)
		})
	}
}

func TestRPCDeclareUnknownType(t *testing.T) {
	r := newTestRPC()

	var ok bool
	err := r.Declare(&NamedCollector{Name: "bad", Collector: Collector{Type: "gaugee"}}, &ok)
	require.ErrorContains(t, err, errUnknownCollectorTyp.Error())

	_, exists := r.p.collectors.Load("bad")
	assert.False(t, exists)
}

func TestRPCDeclareExistingName(t *testing.T) {
	r := newTestRPC()

	declare(t, r, NamedCollector{Name: "same", Collector: Collector{Type: Counter}})
	first, _ := r.p.collectors.Load("same")

	// a repeated declaration reports success and keeps the collector in place
	declare(t, r, NamedCollector{Name: "same", Collector: Collector{Type: Gauge}})
	second, _ := r.p.collectors.Load("same")
	assert.Same(t, first, second)
}

func TestRPCDeclareRegistryConflict(t *testing.T) {
	r := newTestRPC()
	require.NoError(t, r.p.Register(prometheus.NewCounter(prometheus.CounterOpts{Name: "taken"})))

	// the plugin map is free but prometheus already knows the metric name
	var ok bool
	err := r.Declare(&NamedCollector{Name: "taken", Collector: Collector{Type: Counter}}, &ok)
	require.ErrorContains(t, err, "duplicate metrics collector registration attempted")

	_, exists := r.p.collectors.Load("taken")
	assert.False(t, exists)
}

func TestRPCAdd(t *testing.T) {
	r := newTestRPC()
	declare(t, r, NamedCollector{Name: "gauge", Collector: Collector{Type: Gauge}})
	declare(t, r, NamedCollector{Name: "counter", Collector: Collector{Type: Counter}})
	declare(t, r, NamedCollector{Name: "gauge_vec", Collector: Collector{Type: Gauge, Labels: []string{"label"}}})
	declare(t, r, NamedCollector{Name: "counter_vec", Collector: Collector{Type: Counter, Labels: []string{"label"}}})

	var ok bool
	require.NoError(t, r.Add(&Metric{Name: "gauge", Value: 7}, &ok))
	assert.True(t, ok)
	assert.InDelta(t, 7.0, collectedValue(t, r, "gauge"), 0)

	require.NoError(t, r.Add(&Metric{Name: "counter", Value: 3}, &ok))
	assert.InDelta(t, 3.0, collectedValue(t, r, "counter"), 0)

	require.NoError(t, r.Add(&Metric{Name: "gauge_vec", Value: 2, Labels: []string{"first"}}, &ok))
	require.NoError(t, r.Add(&Metric{Name: "counter_vec", Value: 5, Labels: []string{"first"}}, &ok))
	assert.InDelta(t, 2.0, collectedValue(t, r, "gauge_vec"), 0)
	assert.InDelta(t, 5.0, collectedValue(t, r, "counter_vec"), 0)
}

func TestRPCAddNegativeOnCounter(t *testing.T) {
	r := newTestRPC()
	declare(t, r, NamedCollector{Name: "counter", Collector: Collector{Type: Counter}})
	declare(t, r, NamedCollector{Name: "counter_vec", Collector: Collector{Type: Counter, Labels: []string{"label"}}})

	var ok bool
	require.ErrorIs(t, r.Add(&Metric{Name: "counter", Value: -1}, &ok), errNegativeCounter)
	require.ErrorIs(t, r.Add(&Metric{Name: "counter_vec", Value: -1, Labels: []string{"first"}}, &ok), errNegativeCounter)

	// the rejected call leaves the counter where it was
	assert.InDelta(t, 0.0, collectedValue(t, r, "counter"), 0)
}

func TestRPCSub(t *testing.T) {
	r := newTestRPC()
	declare(t, r, NamedCollector{Name: "gauge", Collector: Collector{Type: Gauge}})
	declare(t, r, NamedCollector{Name: "gauge_vec", Collector: Collector{Type: Gauge, Labels: []string{"label"}}})

	var ok bool
	require.NoError(t, r.Add(&Metric{Name: "gauge", Value: 10}, &ok))
	require.NoError(t, r.Sub(&Metric{Name: "gauge", Value: 4}, &ok))
	assert.True(t, ok)
	assert.InDelta(t, 6.0, collectedValue(t, r, "gauge"), 0)

	require.NoError(t, r.Add(&Metric{Name: "gauge_vec", Value: 10, Labels: []string{"first"}}, &ok))
	require.NoError(t, r.Sub(&Metric{Name: "gauge_vec", Value: 4, Labels: []string{"first"}}, &ok))
	assert.InDelta(t, 6.0, collectedValue(t, r, "gauge_vec"), 0)
}

func TestRPCSet(t *testing.T) {
	r := newTestRPC()
	declare(t, r, NamedCollector{Name: "gauge", Collector: Collector{Type: Gauge}})
	declare(t, r, NamedCollector{Name: "gauge_vec", Collector: Collector{Type: Gauge, Labels: []string{"label"}}})

	var ok bool
	require.NoError(t, r.Set(&Metric{Name: "gauge", Value: 42}, &ok))
	assert.True(t, ok)
	assert.InDelta(t, 42.0, collectedValue(t, r, "gauge"), 0)

	require.NoError(t, r.Set(&Metric{Name: "gauge_vec", Value: 42, Labels: []string{"first"}}, &ok))
	assert.InDelta(t, 42.0, collectedValue(t, r, "gauge_vec"), 0)
}

func TestRPCObserve(t *testing.T) {
	r := newTestRPC()
	declare(t, r, NamedCollector{Name: "histogram", Collector: Collector{Type: Histogram, Buckets: []float64{1}}})
	declare(t, r, NamedCollector{Name: "histogram_vec", Collector: Collector{Type: Histogram, Buckets: []float64{1}, Labels: []string{"label"}}})
	declare(t, r, NamedCollector{Name: "summary_vec", Collector: Collector{Type: Summary, Labels: []string{"label"}}})

	var ok bool
	require.NoError(t, r.Observe(&Metric{Name: "histogram", Value: 0.5}, &ok))
	assert.True(t, ok)
	assert.Equal(t, uint64(1), collectedSampleCount(t, r, "histogram"))

	require.NoError(t, r.Observe(&Metric{Name: "histogram_vec", Value: 0.5, Labels: []string{"first"}}, &ok))
	require.NoError(t, r.Observe(&Metric{Name: "summary_vec", Value: 0.5, Labels: []string{"first"}}, &ok))
	assert.Equal(t, uint64(1), collectedSampleCount(t, r, "histogram_vec"))
	assert.Equal(t, uint64(1), collectedSampleCount(t, r, "summary_vec"))
}

func TestRPCObserveScalarSummary(t *testing.T) {
	r := newTestRPC()
	declare(t, r, NamedCollector{Name: "summary", Collector: Collector{Type: Summary}})

	// prometheus.Summary and prometheus.Histogram have the same method set, so a
	// scalar summary is observed through the histogram arm of the type switch
	var ok bool
	require.NoError(t, r.Observe(&Metric{Name: "summary", Value: 0.5}, &ok))
	assert.True(t, ok)
	assert.Equal(t, uint64(1), collectedSampleCount(t, r, "summary"))
}

func TestRPCUnsupportedOperations(t *testing.T) {
	r := newTestRPC()
	declare(t, r, NamedCollector{Name: "gauge", Collector: Collector{Type: Gauge}})
	declare(t, r, NamedCollector{Name: "counter", Collector: Collector{Type: Counter}})
	declare(t, r, NamedCollector{Name: "histogram", Collector: Collector{Type: Histogram, Buckets: []float64{1}}})

	var ok bool
	require.ErrorIs(t, r.Add(&Metric{Name: "histogram", Value: 1}, &ok), errUnsupportedOpForCol)
	require.ErrorIs(t, r.Sub(&Metric{Name: "counter", Value: 1}, &ok), errUnsupportedOpForCol)
	require.ErrorIs(t, r.Set(&Metric{Name: "counter", Value: 1}, &ok), errUnsupportedOpForCol)
	require.ErrorIs(t, r.Observe(&Metric{Name: "gauge", Value: 1}, &ok), errUnsupportedOpForCol)
}

func TestRPCVecCollectorsNeedLabels(t *testing.T) {
	r := newTestRPC()
	declare(t, r, NamedCollector{Name: "gauge_vec", Collector: Collector{Type: Gauge, Labels: []string{"first", "second"}}})
	declare(t, r, NamedCollector{Name: "counter_vec", Collector: Collector{Type: Counter, Labels: []string{"first", "second"}}})
	declare(t, r, NamedCollector{Name: "histogram_vec", Collector: Collector{Type: Histogram, Labels: []string{"first", "second"}}})
	declare(t, r, NamedCollector{Name: "summary_vec", Collector: Collector{Type: Summary, Labels: []string{"first", "second"}}})

	var ok bool
	require.ErrorIs(t, r.Add(&Metric{Name: "gauge_vec", Value: 1}, &ok), errRequiredLabels)
	require.ErrorIs(t, r.Add(&Metric{Name: "counter_vec", Value: 1}, &ok), errRequiredLabels)
	require.ErrorIs(t, r.Sub(&Metric{Name: "gauge_vec", Value: 1}, &ok), errRequiredLabels)
	require.ErrorIs(t, r.Set(&Metric{Name: "gauge_vec", Value: 1}, &ok), errRequiredLabels)
	require.ErrorIs(t, r.Observe(&Metric{Name: "histogram_vec", Value: 1}, &ok), errRequiredLabels)
	require.ErrorIs(t, r.Observe(&Metric{Name: "summary_vec", Value: 1}, &ok), errRequiredLabels)

	// prometheus rejects a label set that does not match the declared cardinality
	require.Error(t, r.Add(&Metric{Name: "gauge_vec", Value: 1, Labels: []string{"only"}}, &ok))
	require.Error(t, r.Observe(&Metric{Name: "summary_vec", Value: 1, Labels: []string{"only"}}, &ok))
}

func TestRPCUndefinedCollector(t *testing.T) {
	r := newTestRPC()

	var ok bool
	require.ErrorIs(t, r.Add(&Metric{Name: "nope"}, &ok), errUndefinedCollector)
	require.ErrorIs(t, r.Sub(&Metric{Name: "nope"}, &ok), errUndefinedCollector)
	require.ErrorIs(t, r.Set(&Metric{Name: "nope"}, &ok), errUndefinedCollector)
	require.ErrorIs(t, r.Observe(&Metric{Name: "nope"}, &ok), errUndefinedCollector)
	require.ErrorIs(t, r.Unregister("nope", &ok), errUndefinedCollector)
}

func TestRPCNilCollectorEntry(t *testing.T) {
	r := newTestRPC()
	r.p.collectors.Store("empty", nil)

	var ok bool
	require.ErrorIs(t, r.Add(&Metric{Name: "empty"}, &ok), errUndefinedCollector)
	require.ErrorIs(t, r.Unregister("empty", &ok), errUndefinedCollector)
}

func TestRPCForeignCollectorEntry(t *testing.T) {
	r := newTestRPC()
	r.p.collectors.Store("foreign", "not a collector")

	// defensive arms, the plugin itself only ever stores *collector values
	var ok bool
	require.ErrorContains(t, r.Add(&Metric{Name: "foreign"}, &ok), "collectors map held non-*collector for foreign")
	require.ErrorContains(t, r.Unregister("foreign", &ok), "collectors map held non-*collector for foreign")
}

func TestRPCUnregister(t *testing.T) {
	r := newTestRPC()
	declare(t, r, NamedCollector{Name: "gone", Collector: Collector{Type: Counter}})

	var ok bool
	require.NoError(t, r.Unregister("gone", &ok))
	assert.True(t, ok)
	assert.NotContains(t, gatheredNames(t, r.p.registry), "gone")

	_, exists := r.p.collectors.Load("gone")
	assert.False(t, exists)
}

func TestRPCUnregisterUnknownToPrometheus(t *testing.T) {
	r := newTestRPC()
	// the collector is in the plugin map but was never given to prometheus
	r.p.collectors.Store("orphan", &collector{col: prometheus.NewCounter(prometheus.CounterOpts{Name: "orphan"})})

	ok := true
	require.NoError(t, r.Unregister("orphan", &ok))
	assert.False(t, ok)

	_, exists := r.p.collectors.Load("orphan")
	assert.False(t, exists)
}

func TestBuildPromCollectorUnknownType(t *testing.T) {
	col, err := buildPromCollector(&NamedCollector{Name: "bad", Collector: Collector{Type: "gaugee"}})
	require.Error(t, err)
	assert.Nil(t, col)
	assert.True(t, errors.Is(err, errUnknownCollectorTyp))
}

func TestRPCLogsThroughPluginLogger(t *testing.T) {
	var buf bytes.Buffer

	p := newTestPlugin()
	p.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r, isRPC := p.RPC().(*rpc)
	require.True(t, isRPC)

	var ok bool
	require.NoError(t, r.Declare(&NamedCollector{Name: "logged", Collector: Collector{Type: Counter}}, &ok))

	// the service writes through the logger the plugin holds, not a default one
	assert.Contains(t, buf.String(), "declaring new metric")
	assert.Contains(t, buf.String(), "name=logged")
}
