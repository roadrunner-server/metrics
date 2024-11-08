package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"
)

type rpc struct {
	p   *Plugin
	log *zap.Logger
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

// Add new metric to the designated collector.
func (r *rpc) Add(m *Metric, ok *bool) error {
	const op = errors.Op("metrics_plugin_add")
	r.log.Debug("adding metric", zap.String("name", m.Name), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))
	c, exist := r.p.collectors.Load(m.Name)
	if !exist {
		r.log.Error("undefined collector", zap.String("collector", m.Name))
		return errors.E(op, errors.Errorf("undefined collector %s, try first Declare the desired collector", m.Name))
	}

	col := c.(*collector)

	switch c := col.col.(type) {
	case prometheus.Gauge:
		c.Add(m.Value)

	case *prometheus.GaugeVec:
		if len(m.Labels) == 0 {
			r.log.Error("required labels for collector", zap.String("collector", m.Name))
			return errors.E(op, errors.Errorf("required labels for collector %s", m.Name))
		}

		gauge, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		gauge.Add(m.Value)
	case prometheus.Counter:
		c.Add(m.Value)

	case *prometheus.CounterVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector `%s`", m.Name))
		}

		gauge, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		gauge.Add(m.Value)

	default:
		return errors.E(op, errors.Errorf("collector %s does not support method `Add`", m.Name))
	}

	// RPC, set ok to true as return value. Need by r.Call reply argument
	*ok = true
	r.log.Debug("metric successfully added", zap.String("name", m.Name), zap.Strings("labels", m.Labels), zap.Float64("value", m.Value))
	return nil
}

// Sub subtract the value from the specific metric (gauge only).
func (r *rpc) Sub(m *Metric, ok *bool) error {
	const op = errors.Op("metrics_plugin_sub")
	r.log.Debug("subtracting value from metric", zap.String("name", m.Name), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))
	c, exist := r.p.collectors.Load(m.Name)
	if !exist {
		r.log.Error("undefined collector", zap.String("name", m.Name), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))
		return errors.E(op, errors.Errorf("undefined collector %s", m.Name))
	}
	if c == nil {
		// can it be a nil ??? I guess can't
		return errors.E(op, errors.Errorf("undefined collector %s", m.Name))
	}

	col := c.(*collector)

	switch c := col.col.(type) {
	case prometheus.Gauge:
		c.Sub(m.Value)

	case *prometheus.GaugeVec:
		if len(m.Labels) == 0 {
			r.log.Error("required labels for collector, but none was provided", zap.String("name", m.Name), zap.Float64("value", m.Value))
			return errors.E(op, errors.Errorf("required labels for collector %s", m.Name))
		}

		gauge, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		gauge.Sub(m.Value)
	default:
		return errors.E(op, errors.Errorf("collector `%s` does not support method `Sub`", m.Name))
	}
	r.log.Debug("subtracting operation finished successfully", zap.String("name", m.Name), zap.Strings("labels", m.Labels), zap.Float64("value", m.Value))

	*ok = true
	return nil
}

// Observe the value (histogram and summary only).
func (r *rpc) Observe(m *Metric, ok *bool) error {
	const op = errors.Op("metrics_plugin_observe")
	r.log.Debug("observing metric", zap.String("name", m.Name), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))

	c, exist := r.p.collectors.Load(m.Name)
	if !exist {
		r.log.Error("undefined collector", zap.String("name", m.Name), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))
		return errors.E(op, errors.Errorf("undefined collector %s", m.Name))
	}
	if c == nil {
		return errors.E(op, errors.Errorf("undefined collector %s", m.Name))
	}

	col := c.(*collector)

	switch c := col.col.(type) {
	case *prometheus.SummaryVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector `%s`", m.Name))
		}

		observer, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			return errors.E(op, err)
		}
		observer.Observe(m.Value)

	case prometheus.Histogram:
		c.Observe(m.Value)

	case *prometheus.HistogramVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector `%s`", m.Name))
		}

		observer, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		observer.Observe(m.Value)
	default:
		return errors.E(op, errors.Errorf("collector `%s` does not support method `Observe`", m.Name))
	}

	r.log.Debug("observe operation finished successfully", zap.String("name", m.Name), zap.Strings("labels", m.Labels), zap.Float64("value", m.Value))

	*ok = true
	return nil
}

// Declare is used to register new collector in prometheus
func (r *rpc) Declare(nc *NamedCollector, ok *bool) error {
	const op = errors.Op("metrics_plugin_declare")
	r.p.mu.Lock()
	defer r.p.mu.Unlock()

	r.log.Debug("declaring new metric", zap.String("name", nc.Name), zap.Any("type", nc.Type), zap.String("namespace", nc.Namespace))
	_, exist := r.p.collectors.Load(nc.Name)
	if exist {
		r.log.Warn("metric with provided name already exist", zap.String("name", nc.Name), zap.Any("type", nc.Type), zap.String("namespace", nc.Namespace))
		*ok = true
		return nil
	}

	var promCol prometheus.Collector
	switch nc.Type {
	case Histogram:
		opts := prometheus.HistogramOpts{
			Name:      nc.Name,
			Namespace: nc.Namespace,
			Subsystem: nc.Subsystem,
			Help:      nc.Help,
			Buckets:   nc.Buckets,
		}

		if len(nc.Labels) != 0 {
			promCol = prometheus.NewHistogramVec(opts, nc.Labels)
		} else {
			promCol = prometheus.NewHistogram(opts)
		}
	case Gauge:
		opts := prometheus.GaugeOpts{
			Name:      nc.Name,
			Namespace: nc.Namespace,
			Subsystem: nc.Subsystem,
			Help:      nc.Help,
		}

		if len(nc.Labels) != 0 {
			promCol = prometheus.NewGaugeVec(opts, nc.Labels)
		} else {
			promCol = prometheus.NewGauge(opts)
		}
	case Counter:
		opts := prometheus.CounterOpts{
			Name:      nc.Name,
			Namespace: nc.Namespace,
			Subsystem: nc.Subsystem,
			Help:      nc.Help,
		}

		if len(nc.Labels) != 0 {
			promCol = prometheus.NewCounterVec(opts, nc.Labels)
		} else {
			promCol = prometheus.NewCounter(opts)
		}
	case Summary:
		opts := prometheus.SummaryOpts{
			Name:      nc.Name,
			Namespace: nc.Namespace,
			Subsystem: nc.Subsystem,
			Help:      nc.Help,
		}

		if len(nc.Labels) != 0 {
			promCol = prometheus.NewSummaryVec(opts, nc.Labels)
		} else {
			promCol = prometheus.NewSummary(opts)
		}

	default:
		return errors.E(op, errors.Errorf("unknown collector type %s", nc.Type))
	}

	// that method might panic, we handle it by recover
	err := r.p.Register(promCol)
	if err != nil {
		*ok = false
		return errors.E(op, err)
	}

	col := &collector{
		col:        promCol,
		registered: true,
	}

	// add collector to sync.Map
	r.p.collectors.Store(nc.Name, col)

	r.log.Debug("metric successfully added", zap.String("name", nc.Name), zap.Any("type", nc.Type), zap.String("namespace", nc.Namespace))

	*ok = true
	return nil
}

// Unregister removes collector from the prometheus registry
func (r *rpc) Unregister(name string, ok *bool) error {
	const op = errors.Op("metrics_plugin_unregister")

	r.log.Debug("unregistering collector", zap.String("name", name))

	c, exist := r.p.collectors.LoadAndDelete(name)
	if !exist || c == nil {
		return errors.E(op, errors.Errorf("undefined collector %s", name))
	}

	if col, k := c.(*collector); k {
		if r.p.registry.Unregister(col.col) {
			*ok = true
			r.log.Debug("collector was successfully unregistered", zap.String("name", name))
			return nil
		}

		r.log.Debug("collector was deleted from the RR registry but not from the prometheus collector", zap.String("name", name))
	}

	return nil
}

// Set the metric value (only for gaude).
func (r *rpc) Set(m *Metric, ok *bool) (err error) {
	const op = errors.Op("metrics_plugin_set")
	r.log.Debug("observing metric", zap.String("name", m.Name), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))

	c, exist := r.p.collectors.Load(m.Name)
	if !exist {
		return errors.E(op, errors.Errorf("undefined collector %s", m.Name))
	}
	if c == nil {
		return errors.E(op, errors.Errorf("undefined collector %s", m.Name))
	}

	col := c.(*collector)

	switch c := col.col.(type) {
	case prometheus.Gauge:
		c.Set(m.Value)

	case *prometheus.GaugeVec:
		if len(m.Labels) == 0 {
			r.log.Error("required labels for collector", zap.String("collector", m.Name))
			return errors.E(op, errors.Errorf("required labels for collector %s", m.Name))
		}
		gauge, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		gauge.Set(m.Value)

	default:
		return errors.E(op, errors.Errorf("collector `%s` does not support method Set", m.Name))
	}

	r.log.Debug("set operation finished successfully", zap.String("name", m.Name), zap.Strings("labels", m.Labels), zap.Float64("value", m.Value))

	*ok = true
	return nil
}
