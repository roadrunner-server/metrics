package metrics

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	rrerrors "github.com/roadrunner-server/errors"
)

var (
	errUndefinedCollector  = errors.New("undefined collector")
	errRequiredLabels      = errors.New("required labels for collector")
	errUnsupportedOpForCol = errors.New("collector does not support the requested operation")
	errUnknownCollectorTyp = errors.New("unknown collector type")
	errNegativeCounter     = errors.New("counter cannot decrease in value")
)

type rpc struct {
	p   *Plugin
	log *slog.Logger
}

// Metric represents a single metric produced by the application.
type Metric struct {
	// Collector name.
	Name string `msgpack:"alias:name"`
	// Collector value.
	Value float64 `msgpack:"alias:value"`
	// Labels associated with metric. Only for vector metrics. Must be provided in a form of label values.
	Labels []string `msgpack:"alias:labels"`
}

// Add the value to the specific metric (gauge and counter).
func (r *rpc) Add(m *Metric, ok *bool) error {
	r.log.Debug("adding metric", "name", m.Name, "value", m.Value, "labels", m.Labels)

	col, err := r.lookupCollector(m.Name)
	if err != nil {
		return err
	}

	switch c := col.(type) {
	case prometheus.Gauge:
		c.Add(m.Value)
	case *prometheus.GaugeVec:
		gv, err := vecMetric(r, c, m)
		if err != nil {
			return err
		}
		gv.Add(m.Value)
	case prometheus.Counter:
		if m.Value < 0 {
			return fmt.Errorf("%w: %s", errNegativeCounter, m.Name)
		}
		c.Add(m.Value)
	case *prometheus.CounterVec:
		if m.Value < 0 {
			return fmt.Errorf("%w: %s", errNegativeCounter, m.Name)
		}
		cv, err := vecMetric(r, c, m)
		if err != nil {
			return err
		}
		cv.Add(m.Value)
	default:
		return fmt.Errorf("%w: %s does not support Add", errUnsupportedOpForCol, m.Name)
	}

	r.log.Debug("metric successfully added", "name", m.Name, "labels", m.Labels, "value", m.Value)
	*ok = true
	return nil
}

// Sub subtracts the value from the specific metric (gauge only).
func (r *rpc) Sub(m *Metric, ok *bool) error {
	r.log.Debug("subtracting value from metric", "name", m.Name, "value", m.Value, "labels", m.Labels)

	col, err := r.lookupCollector(m.Name)
	if err != nil {
		return err
	}

	switch c := col.(type) {
	case prometheus.Gauge:
		c.Sub(m.Value)
	case *prometheus.GaugeVec:
		gv, err := vecMetric(r, c, m)
		if err != nil {
			return err
		}
		gv.Sub(m.Value)
	default:
		return fmt.Errorf("%w: %s does not support Sub", errUnsupportedOpForCol, m.Name)
	}

	r.log.Debug("subtracting operation finished successfully", "name", m.Name, "labels", m.Labels, "value", m.Value)
	*ok = true
	return nil
}

// Observe the value (histogram and summary only).
func (r *rpc) Observe(m *Metric, ok *bool) error {
	r.log.Debug("observing metric", "name", m.Name, "value", m.Value, "labels", m.Labels)

	col, err := r.lookupCollector(m.Name)
	if err != nil {
		return err
	}

	switch c := col.(type) {
	// prometheus.Histogram and prometheus.Summary have identical method sets
	// (Metric + Collector + Observe(float64)), so scalar Summary instances
	// also match this branch — type-switch picks the first matching interface
	// in source order.
	case prometheus.Histogram:
		c.Observe(m.Value)
	case *prometheus.HistogramVec:
		ov, err := vecMetric[prometheus.Observer](r, c, m)
		if err != nil {
			return err
		}
		ov.Observe(m.Value)
	case *prometheus.SummaryVec:
		ov, err := vecMetric[prometheus.Observer](r, c, m)
		if err != nil {
			return err
		}
		ov.Observe(m.Value)
	default:
		return fmt.Errorf("%w: %s does not support Observe", errUnsupportedOpForCol, m.Name)
	}

	r.log.Debug("observe operation finished successfully", "name", m.Name, "labels", m.Labels, "value", m.Value)
	*ok = true
	return nil
}

// Set the metric value (gauge only).
func (r *rpc) Set(m *Metric, ok *bool) error {
	r.log.Debug("setting metric", "name", m.Name, "value", m.Value, "labels", m.Labels)

	col, err := r.lookupCollector(m.Name)
	if err != nil {
		return err
	}

	switch c := col.(type) {
	case prometheus.Gauge:
		c.Set(m.Value)
	case *prometheus.GaugeVec:
		gv, err := vecMetric(r, c, m)
		if err != nil {
			return err
		}
		gv.Set(m.Value)
	default:
		return fmt.Errorf("%w: %s does not support Set", errUnsupportedOpForCol, m.Name)
	}

	r.log.Debug("set operation finished successfully", "name", m.Name, "labels", m.Labels, "value", m.Value)
	*ok = true
	return nil
}

// Declare is used to register a new collector in prometheus.
func (r *rpc) Declare(nc *NamedCollector, ok *bool) error {
	const op = rrerrors.Op("metrics_rpc_declare")

	r.p.mu.Lock()
	defer r.p.mu.Unlock()

	r.log.Debug("declaring new metric", "name", nc.Name, "type", nc.Type, "namespace", nc.Namespace)
	if _, exist := r.p.collectors.Load(nc.Name); exist {
		r.log.Warn("metric with provided name already exist", "name", nc.Name)
		*ok = true
		return nil
	}

	promCol, err := buildPromCollector(nc)
	if err != nil {
		return rrerrors.E(op, err)
	}

	if err := r.p.Register(promCol); err != nil {
		return rrerrors.E(op, err)
	}

	r.p.collectors.Store(nc.Name, &collector{col: promCol, registered: true})
	r.log.Debug("metric successfully added", "name", nc.Name, "type", nc.Type, "namespace", nc.Namespace)
	*ok = true
	return nil
}

// Unregister removes the collector from the prometheus registry.
func (r *rpc) Unregister(name string, ok *bool) error {
	r.log.Debug("unregistering collector", "name", name)

	c, exist := r.p.collectors.LoadAndDelete(name)
	if !exist || c == nil {
		return fmt.Errorf("%w: %s", errUndefinedCollector, name)
	}

	col, k := c.(*collector)
	if !k {
		return fmt.Errorf("collectors map held non-*collector for %s", name)
	}
	if r.p.registry.Unregister(col.col) {
		r.log.Debug("collector was successfully unregistered", "name", name)
		*ok = true
		return nil
	}
	// Preserves legacy contract: prometheus refused to unregister (already
	// gone, or never registered there). The collector is removed from our map
	// either way, but the caller deserves to know prometheus state diverged.
	r.log.Debug("collector was deleted from the RR registry but not from the prometheus collector", "name", name)
	*ok = false
	return nil
}

func (r *rpc) lookupCollector(name string) (prometheus.Collector, error) {
	c, exist := r.p.collectors.Load(name)
	if !exist || c == nil {
		r.log.Error("undefined collector", "collector", name)
		return nil, fmt.Errorf("%w: %s", errUndefinedCollector, name)
	}
	col, ok := c.(*collector)
	if !ok {
		return nil, fmt.Errorf("collectors map held non-*collector for %s", name)
	}
	return col.col, nil
}

func vecMetric[V any, T interface {
	GetMetricWithLabelValues(lvs ...string) (V, error)
}](r *rpc, c T, m *Metric) (V, error) {
	var zero V
	if len(m.Labels) == 0 {
		r.log.Error("required labels for collector", "collector", m.Name)
		return zero, fmt.Errorf("%w: %s", errRequiredLabels, m.Name)
	}
	v, err := c.GetMetricWithLabelValues(m.Labels...)
	if err != nil {
		r.log.Error("failed to get metrics with label values", "collector", m.Name, "labels", m.Labels)
		return zero, err
	}
	return v, nil
}

func buildPromCollector(nc *NamedCollector) (prometheus.Collector, error) {
	switch nc.Type {
	case Histogram:
		opts := prometheus.HistogramOpts{Name: nc.Name, Namespace: nc.Namespace, Subsystem: nc.Subsystem, Help: nc.Help, Buckets: nc.Buckets}
		if len(nc.Labels) != 0 {
			return prometheus.NewHistogramVec(opts, nc.Labels), nil
		}
		return prometheus.NewHistogram(opts), nil
	case Gauge:
		opts := prometheus.GaugeOpts{Name: nc.Name, Namespace: nc.Namespace, Subsystem: nc.Subsystem, Help: nc.Help}
		if len(nc.Labels) != 0 {
			return prometheus.NewGaugeVec(opts, nc.Labels), nil
		}
		return prometheus.NewGauge(opts), nil
	case Counter:
		opts := prometheus.CounterOpts{Name: nc.Name, Namespace: nc.Namespace, Subsystem: nc.Subsystem, Help: nc.Help}
		if len(nc.Labels) != 0 {
			return prometheus.NewCounterVec(opts, nc.Labels), nil
		}
		return prometheus.NewCounter(opts), nil
	case Summary:
		opts := prometheus.SummaryOpts{Name: nc.Name, Namespace: nc.Namespace, Subsystem: nc.Subsystem, Help: nc.Help, Objectives: nc.Objectives}
		if len(nc.Labels) != 0 {
			return prometheus.NewSummaryVec(opts, nc.Labels), nil
		}
		return prometheus.NewSummary(opts), nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnknownCollectorTyp, nc.Type)
	}
}
