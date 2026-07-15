package metrics

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	metricsV1 "github.com/roadrunner-server/api-go/v6/metrics/v1"
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

func (r *rpc) Add(in *metricsV1.AddRequest, out *metricsV1.Response) error {
	m := in.GetMetric()
	r.log.Debug("adding metric", "name", m.GetName(), "value", m.GetValue(), "labels", m.GetLabels())

	col, err := r.lookupCollector(m.GetName())
	if err != nil {
		return err
	}

	switch c := col.(type) {
	case prometheus.Gauge:
		c.Add(m.GetValue())
	case *prometheus.GaugeVec:
		gv, err := vecMetric(r, c, m)
		if err != nil {
			return err
		}
		gv.Add(m.GetValue())
	case prometheus.Counter:
		if m.GetValue() < 0 {
			return fmt.Errorf("%w: %s", errNegativeCounter, m.GetName())
		}
		c.Add(m.GetValue())
	case *prometheus.CounterVec:
		if m.GetValue() < 0 {
			return fmt.Errorf("%w: %s", errNegativeCounter, m.GetName())
		}
		cv, err := vecMetric(r, c, m)
		if err != nil {
			return err
		}
		cv.Add(m.GetValue())
	default:
		return fmt.Errorf("%w: %s does not support Add", errUnsupportedOpForCol, m.GetName())
	}

	r.log.Debug("metric successfully added", "name", m.GetName(), "labels", m.GetLabels(), "value", m.GetValue())
	out.Ok = true
	return nil
}

func (r *rpc) Sub(in *metricsV1.SubRequest, out *metricsV1.Response) error {
	m := in.GetMetric()
	r.log.Debug("subtracting value from metric", "name", m.GetName(), "value", m.GetValue(), "labels", m.GetLabels())

	col, err := r.lookupCollector(m.GetName())
	if err != nil {
		return err
	}

	switch c := col.(type) {
	case prometheus.Gauge:
		c.Sub(m.GetValue())
	case *prometheus.GaugeVec:
		gv, err := vecMetric(r, c, m)
		if err != nil {
			return err
		}
		gv.Sub(m.GetValue())
	default:
		return fmt.Errorf("%w: %s does not support Sub", errUnsupportedOpForCol, m.GetName())
	}

	r.log.Debug("subtracting operation finished successfully", "name", m.GetName(), "labels", m.GetLabels(), "value", m.GetValue())
	out.Ok = true
	return nil
}

func (r *rpc) Observe(in *metricsV1.ObserveRequest, out *metricsV1.Response) error {
	m := in.GetMetric()
	r.log.Debug("observing metric", "name", m.GetName(), "value", m.GetValue(), "labels", m.GetLabels())

	col, err := r.lookupCollector(m.GetName())
	if err != nil {
		return err
	}

	switch c := col.(type) {
	// prometheus.Histogram and prometheus.Summary have identical method sets
	// (Metric + Collector + Observe(float64)), so scalar Summary instances
	// also match this branch — type-switch picks the first matching interface
	// in source order.
	case prometheus.Histogram:
		c.Observe(m.GetValue())
	case *prometheus.HistogramVec:
		ov, err := vecMetric[prometheus.Observer](r, c, m)
		if err != nil {
			return err
		}
		ov.Observe(m.GetValue())
	case *prometheus.SummaryVec:
		ov, err := vecMetric[prometheus.Observer](r, c, m)
		if err != nil {
			return err
		}
		ov.Observe(m.GetValue())
	default:
		return fmt.Errorf("%w: %s does not support Observe", errUnsupportedOpForCol, m.GetName())
	}

	r.log.Debug("observe operation finished successfully", "name", m.GetName(), "labels", m.GetLabels(), "value", m.GetValue())
	out.Ok = true
	return nil
}

func (r *rpc) Set(in *metricsV1.SetRequest, out *metricsV1.Response) error {
	m := in.GetMetric()
	r.log.Debug("setting metric", "name", m.GetName(), "value", m.GetValue(), "labels", m.GetLabels())

	col, err := r.lookupCollector(m.GetName())
	if err != nil {
		return err
	}

	switch c := col.(type) {
	case prometheus.Gauge:
		c.Set(m.GetValue())
	case *prometheus.GaugeVec:
		gv, err := vecMetric(r, c, m)
		if err != nil {
			return err
		}
		gv.Set(m.GetValue())
	default:
		return fmt.Errorf("%w: %s does not support Set", errUnsupportedOpForCol, m.GetName())
	}

	r.log.Debug("set operation finished successfully", "name", m.GetName(), "labels", m.GetLabels(), "value", m.GetValue())
	out.Ok = true
	return nil
}

func (r *rpc) Declare(in *metricsV1.DeclareRequest, out *metricsV1.Response) error {
	const op = rrerrors.Op("metrics_rpc_declare")

	nc := in.GetCollector()
	r.p.mu.Lock()
	defer r.p.mu.Unlock()

	r.log.Debug("declaring new metric", "name", nc.GetName(), "type", nc.GetCollector().GetType(), "namespace", nc.GetCollector().GetNamespace())
	if _, exist := r.p.collectors.Load(nc.GetName()); exist {
		r.log.Warn("metric with provided name already exist", "name", nc.GetName())
		out.Ok = true
		return nil
	}

	promCol, err := buildPromCollector(nc)
	if err != nil {
		return rrerrors.E(op, err)
	}

	if err := r.p.Register(promCol); err != nil {
		return rrerrors.E(op, err)
	}

	r.p.collectors.Store(nc.GetName(), &collector{col: promCol, registered: true})
	r.log.Debug("metric successfully added", "name", nc.GetName(), "type", nc.GetCollector().GetType(), "namespace", nc.GetCollector().GetNamespace())
	out.Ok = true
	return nil
}

func (r *rpc) Unregister(in *metricsV1.UnregisterRequest, out *metricsV1.Response) error {
	name := in.GetName()
	r.log.Debug("unregistering collector", "name", name)

	c, exist := r.p.collectors.LoadAndDelete(name)
	if !exist || c == nil {
		return fmt.Errorf("%w: %s", errUndefinedCollector, name)
	}

	col, ok := c.(*collector)
	if !ok {
		return fmt.Errorf("collectors map held non-*collector for %s", name)
	}
	if r.p.registry.Unregister(col.col) {
		r.log.Debug("collector was successfully unregistered", "name", name)
		out.Ok = true
		return nil
	}
	// Preserves legacy contract: prometheus refused to unregister (already
	// gone, or never registered there). The collector is removed from our map
	// either way, but the caller deserves to know prometheus state diverged.
	r.log.Debug("collector was deleted from the RR registry but not from the prometheus collector", "name", name)
	out.Ok = false
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
}](r *rpc, c T, m *metricsV1.Metric) (V, error) {
	var zero V
	if len(m.GetLabels()) == 0 {
		r.log.Error("required labels for collector", "collector", m.GetName())
		return zero, fmt.Errorf("%w: %s", errRequiredLabels, m.GetName())
	}
	v, err := c.GetMetricWithLabelValues(m.GetLabels()...)
	if err != nil {
		r.log.Error("failed to get metrics with label values", "collector", m.GetName(), "labels", m.GetLabels())
		return zero, err
	}
	return v, nil
}

func buildPromCollector(nc *metricsV1.NamedCollector) (prometheus.Collector, error) {
	col := nc.GetCollector()
	switch col.GetType() {
	case metricsV1.CollectorType_COLLECTOR_TYPE_HISTOGRAM:
		opts := prometheus.HistogramOpts{Name: nc.GetName(), Namespace: col.GetNamespace(), Subsystem: col.GetSubsystem(), Help: col.GetHelp(), Buckets: col.GetBuckets()}
		if len(col.GetLabels()) != 0 {
			return prometheus.NewHistogramVec(opts, col.GetLabels()), nil
		}
		return prometheus.NewHistogram(opts), nil
	case metricsV1.CollectorType_COLLECTOR_TYPE_GAUGE:
		opts := prometheus.GaugeOpts{Name: nc.GetName(), Namespace: col.GetNamespace(), Subsystem: col.GetSubsystem(), Help: col.GetHelp()}
		if len(col.GetLabels()) != 0 {
			return prometheus.NewGaugeVec(opts, col.GetLabels()), nil
		}
		return prometheus.NewGauge(opts), nil
	case metricsV1.CollectorType_COLLECTOR_TYPE_COUNTER:
		opts := prometheus.CounterOpts{Name: nc.GetName(), Namespace: col.GetNamespace(), Subsystem: col.GetSubsystem(), Help: col.GetHelp()}
		if len(col.GetLabels()) != 0 {
			return prometheus.NewCounterVec(opts, col.GetLabels()), nil
		}
		return prometheus.NewCounter(opts), nil
	case metricsV1.CollectorType_COLLECTOR_TYPE_SUMMARY:
		var objectives map[float64]float64
		if raw := col.GetObjectives(); len(raw) > 0 {
			objectives = make(map[float64]float64, len(raw))
			for _, o := range raw {
				objectives[o.GetQuantile()] = o.GetError()
			}
		}
		opts := prometheus.SummaryOpts{Name: nc.GetName(), Namespace: col.GetNamespace(), Subsystem: col.GetSubsystem(), Help: col.GetHelp(), Objectives: objectives}
		if len(col.GetLabels()) != 0 {
			return prometheus.NewSummaryVec(opts, col.GetLabels()), nil
		}
		return prometheus.NewSummary(opts), nil
	case metricsV1.CollectorType_COLLECTOR_TYPE_UNSPECIFIED:
		return nil, fmt.Errorf("%w: unspecified", errUnknownCollectorTyp)
	default:
		return nil, fmt.Errorf("%w: %v", errUnknownCollectorTyp, col.GetType())
	}
}
