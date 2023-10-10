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
	// Collector namespace.
	Namespace string `msgpack:"alias:namespace"`
	// Collector value.
	Value float64 `msgpack:"alias:value"`
	// Labels associated with metric. Only for vector metrics. Must be provided in the form of label values.
	Labels []string `msgpack:"alias:labels"`
}

// Add new metric to the designated collector.
func (r *rpc) Add(m *Metric, ok *bool) error {
	const op = errors.Op("metrics_plugin_add")

	if m.Namespace == "" {
		r.log.Warn("namespace is missing")
	}

	r.log.Debug("adding metric", zap.String("name", m.Name), zap.String("namespace", m.Namespace), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))

	col, exist := r.collectorExists(m.Name, m.Namespace)
	if !exist {
		r.log.Error("undefined collector", zap.String("collector", m.Name), zap.String("namespace", m.Namespace))
		return errors.E(op, errors.Errorf("undefined collector %s, with namespace: %s, try first Declare the desired collector", m.Name, m.Namespace))
	}

	if col == nil {
		// can it be a nil ??? I guess can't
		return errors.E(op, errors.Errorf("undefined collector, name: %s, namespace: %s", m.Name, m.Namespace))
	}

	switch c := col.col.(type) {
	case prometheus.Gauge:
		c.Add(m.Value)

	case *prometheus.GaugeVec:
		if len(m.Labels) == 0 {
			r.log.Error("required labels for collector", zap.String("collector", m.Name), zap.String("namespace", m.Namespace))
			return errors.E(op, errors.Errorf("required labels for collector, name: %s, namespace: %s", m.Name, m.Namespace))
		}

		gauge, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.String("namespace", m.Namespace), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		gauge.Add(m.Value)
	case prometheus.Counter:
		c.Add(m.Value)

	case *prometheus.CounterVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector, name: %s, namespace: %s", m.Name, m.Namespace))
		}

		gauge, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.String("namespace", m.Namespace), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		gauge.Add(m.Value)

	default:
		return errors.E(op, errors.Errorf("collector, name: %s with namespace: %s, does not support method `Add`", m.Name, m.Namespace))
	}

	// RPC, set ok to true as return value. Need by r.Call reply argument
	*ok = true
	r.log.Debug("metric successfully added", zap.String("name", m.Name), zap.Strings("labels", m.Labels), zap.String("namespace", m.Namespace), zap.Float64("value", m.Value))
	return nil
}

// Sub subtract the value from the specific metric (gauge only).
func (r *rpc) Sub(m *Metric, ok *bool) error {
	const op = errors.Op("metrics_plugin_sub")
	r.log.Debug("subtracting value from metric", zap.String("name", m.Name), zap.String("namespace", m.Namespace), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))
	col, exist := r.collectorExists(m.Name, m.Namespace)
	if !exist {
		r.log.Error("undefined collector", zap.String("name", m.Name), zap.String("namespace", m.Namespace), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))
		return errors.E(op, errors.Errorf("undefined collector, name: %s, namespace: %s", m.Name, m.Namespace))
	}
	if col == nil {
		// can it be a nil ??? I guess can't
		return errors.E(op, errors.Errorf("undefined collector, name: %s, namespace: %s", m.Name, m.Namespace))
	}

	switch c := col.col.(type) {
	case prometheus.Gauge:
		c.Sub(m.Value)

	case *prometheus.GaugeVec:
		if len(m.Labels) == 0 {
			r.log.Error("required labels for collector, but none was provided", zap.String("name", m.Name), zap.String("namespace", m.Namespace), zap.Float64("value", m.Value))
			return errors.E(op, errors.Errorf("required labels for collector, name: %s, namespace: %s", m.Name, m.Namespace))
		}

		gauge, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.String("namespace", m.Namespace), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		gauge.Sub(m.Value)
	default:
		return errors.E(op, errors.Errorf("collector doesn't support method 'Sub', name: %s, namespace: %s", m.Name, m.Namespace))
	}
	r.log.Debug("subtracting operation finished successfully", zap.String("name", m.Name), zap.String("namespace", m.Namespace), zap.Strings("labels", m.Labels), zap.Float64("value", m.Value))

	*ok = true
	return nil
}

// Observe the value (histogram and summary only).
func (r *rpc) Observe(m *Metric, ok *bool) error {
	const op = errors.Op("metrics_plugin_observe")
	r.log.Debug("observing metric", zap.String("name", m.Name), zap.String("namespace", m.Namespace), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))

	col, exist := r.collectorExists(m.Name, m.Namespace)
	if !exist {
		r.log.Error("undefined collector", zap.String("name", m.Name), zap.String("namespace", m.Namespace), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))
		return errors.E(op, errors.Errorf("undefined collector, name: %s, namespace: %s", m.Name, m.Namespace))
	}
	if col == nil {
		return errors.E(op, errors.Errorf("undefined collector, name: %s, namespace: %s", m.Name, m.Namespace))
	}

	switch c := col.col.(type) {
	case *prometheus.SummaryVec:
		if len(m.Labels) == 0 {
			return errors.E(op, errors.Errorf("required labels for collector, name: %s, namespace: %s", m.Name, m.Namespace))
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
			return errors.E(op, errors.Errorf("required labels for collector, name: %s, namespace: %s", m.Name, m.Namespace))
		}

		observer, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.String("namespace", m.Namespace), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		observer.Observe(m.Value)
	default:
		return errors.E(op, errors.Errorf("collector doesn't support method 'Observe', name: %s, namespace: %s, supported collectors: SummaryVec, Histogram and HistogramVec", m.Name, m.Namespace))
	}

	r.log.Debug("observe operation finished successfully", zap.String("name", m.Name), zap.String("namespace", m.Namespace), zap.Strings("labels", m.Labels), zap.Float64("value", m.Value))

	*ok = true
	return nil
}

// Declare is used to register new collector in prometheus
func (r *rpc) Declare(nc *NamedCollector, ok *bool) error {
	const op = errors.Op("metrics_plugin_declare")
	r.p.mu.Lock()
	defer r.p.mu.Unlock()

	r.log.Debug("declaring new metric", zap.String("name", nc.Name), zap.String("namespace", nc.Namespace), zap.Any("type", nc.Type))
	_, exist := r.collectorExists(nc.Name, nc.Namespace)
	if exist {
		r.log.Error("metric with provided name already exist", zap.String("name", nc.Name), zap.Any("type", nc.Type), zap.String("namespace", nc.Namespace))
		return errors.E(op, errors.Errorf("tried to register existing collector, name: %s, namespace: %s", nc.Name, nc.Namespace))
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
	r.p.collectors.Store(collectorKey(nc.Name, nc.Namespace), col)

	r.log.Debug("metric successfully added", zap.String("name", nc.Name), zap.String("namespace", nc.Namespace), zap.Any("type", nc.Type))

	*ok = true
	return nil
}

// Unregister removes collector from the prometheus registry
func (r *rpc) Unregister(name string, ok *bool) error {
	const op = errors.Op("metrics_plugin_unregister")

	r.log.Debug("unregistering collector", zap.String("name", name))

	col, exist := r.collectorExists(name, "")
	if !exist || col == nil {
		return errors.E(op, errors.Errorf("undefined collector %s", name))
	}

	if r.p.registry.Unregister(col.col) {
		*ok = true
		r.log.Debug("collector was successfully unregistered", zap.String("name", name))
		return nil
	}

	r.log.Debug("collector was deleted from the RR registry but not from the prometheus collector", zap.String("name", name), zap.String("namespace", ""))

	return nil
}

// Set the metric value (only for gaude).
func (r *rpc) Set(m *Metric, ok *bool) error {
	const op = errors.Op("metrics_plugin_set")
	r.log.Debug("observing metric", zap.String("name", m.Name), zap.String("namespace", m.Namespace), zap.Float64("value", m.Value), zap.Strings("labels", m.Labels))

	col, exist := r.collectorExists(m.Name, m.Namespace)
	if !exist {
		return errors.E(op, errors.Errorf("undefined collector, name: %s, namespace: %s", m.Name, m.Namespace))
	}
	if col == nil {
		return errors.E(op, errors.Errorf("undefined collector, name: %s, namespace: %s", m.Name, m.Namespace))
	}

	switch c := col.col.(type) {
	case prometheus.Gauge:
		c.Set(m.Value)

	case *prometheus.GaugeVec:
		if len(m.Labels) == 0 {
			r.log.Error("required labels for collector", zap.String("collector", m.Name), zap.String("namespace", m.Namespace))
			return errors.E(op, errors.Errorf("required labels for collector, name: %s, namespace: %s", m.Name, m.Namespace))
		}
		gauge, err := c.GetMetricWithLabelValues(m.Labels...)
		if err != nil {
			r.log.Error("failed to get metrics with label values", zap.String("collector", m.Name), zap.String("namespace", m.Namespace), zap.Strings("labels", m.Labels))
			return errors.E(op, err)
		}
		gauge.Set(m.Value)

	default:
		return errors.E(op, errors.Errorf("collector doesn't support method 'Set', name: %s, namespace: %s, supported collectors: Gauge, GaugeVec", m.Name, m.Namespace))
	}

	r.log.Debug("set operation finished successfully", zap.String("name", m.Name), zap.Strings("labels", m.Labels), zap.Float64("value", m.Value))

	*ok = true
	return nil
}

// here we need to check both approaches, old - by name, new - by name and namespace
func (r *rpc) collectorExists(name, namespace string) (*collector, bool) {
	var c any
	var exists bool
	c, exists = r.p.collectors.Load(name)
	if !exists {
		c, exists = r.p.collectors.Load(collectorKey(name, namespace))
		if !exists {
			return nil, false
		}
	}

	return c.(*collector), true
}
