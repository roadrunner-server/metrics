package metrics

import (
	"context"
	stderr "errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/prometheus/client_golang/prometheus"
	metricsV1 "github.com/roadrunner-server/api-go/v6/metrics/v1"
	"github.com/roadrunner-server/errors"
)

var (
	errUndefinedCollector  = stderr.New("undefined collector")
	errRequiredLabels      = stderr.New("required labels for collector")
	errUnsupportedOpForCol = stderr.New("collector does not support the requested operation")
	errUnknownCollectorTyp = stderr.New("unknown collector type")
)

type rpc struct {
	p   *Plugin
	log *slog.Logger
}

func (r *rpc) Add(_ context.Context, req *connect.Request[metricsV1.AddRequest]) (*connect.Response[metricsV1.Response], error) {
	m := req.Msg.GetMetric()
	r.log.Debug("adding metric", "name", m.GetName(), "value", m.GetValue(), "labels", m.GetLabels())

	col, code, err := r.lookupCollector(m.GetName())
	if err != nil {
		return nil, connect.NewError(code, err)
	}

	switch c := col.(type) {
	case prometheus.Gauge:
		c.Add(m.GetValue())
	case *prometheus.GaugeVec:
		gv, err := vecMetric(r, c, m)
		if err != nil {
			return nil, err
		}
		gv.Add(m.GetValue())
	case prometheus.Counter:
		c.Add(m.GetValue())
	case *prometheus.CounterVec:
		cv, err := vecMetric(r, c, m)
		if err != nil {
			return nil, err
		}
		cv.Add(m.GetValue())
	default:
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("%w: %s does not support Add", errUnsupportedOpForCol, m.GetName()))
	}

	r.log.Debug("metric successfully added", "name", m.GetName(), "labels", m.GetLabels(), "value", m.GetValue())
	return connect.NewResponse(&metricsV1.Response{Ok: true}), nil
}

func (r *rpc) Sub(_ context.Context, req *connect.Request[metricsV1.SubRequest]) (*connect.Response[metricsV1.Response], error) {
	m := req.Msg.GetMetric()
	r.log.Debug("subtracting value from metric", "name", m.GetName(), "value", m.GetValue(), "labels", m.GetLabels())

	col, code, err := r.lookupCollector(m.GetName())
	if err != nil {
		return nil, connect.NewError(code, err)
	}

	switch c := col.(type) {
	case prometheus.Gauge:
		c.Sub(m.GetValue())
	case *prometheus.GaugeVec:
		gv, err := vecMetric(r, c, m)
		if err != nil {
			return nil, err
		}
		gv.Sub(m.GetValue())
	default:
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("%w: %s does not support Sub", errUnsupportedOpForCol, m.GetName()))
	}

	r.log.Debug("subtracting operation finished successfully", "name", m.GetName(), "labels", m.GetLabels(), "value", m.GetValue())
	return connect.NewResponse(&metricsV1.Response{Ok: true}), nil
}

func (r *rpc) Observe(_ context.Context, req *connect.Request[metricsV1.ObserveRequest]) (*connect.Response[metricsV1.Response], error) {
	m := req.Msg.GetMetric()
	r.log.Debug("observing metric", "name", m.GetName(), "value", m.GetValue(), "labels", m.GetLabels())

	col, code, err := r.lookupCollector(m.GetName())
	if err != nil {
		return nil, connect.NewError(code, err)
	}

	switch c := col.(type) {
	case prometheus.Histogram:
		c.Observe(m.GetValue())
	case *prometheus.HistogramVec:
		ov, err := vecMetric[prometheus.Observer](r, c, m)
		if err != nil {
			return nil, err
		}
		ov.Observe(m.GetValue())
	case *prometheus.SummaryVec:
		ov, err := vecMetric[prometheus.Observer](r, c, m)
		if err != nil {
			return nil, err
		}
		ov.Observe(m.GetValue())
	default:
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("%w: %s does not support Observe", errUnsupportedOpForCol, m.GetName()))
	}

	r.log.Debug("observe operation finished successfully", "name", m.GetName(), "labels", m.GetLabels(), "value", m.GetValue())
	return connect.NewResponse(&metricsV1.Response{Ok: true}), nil
}

func (r *rpc) Set(_ context.Context, req *connect.Request[metricsV1.SetRequest]) (*connect.Response[metricsV1.Response], error) {
	m := req.Msg.GetMetric()
	r.log.Debug("setting metric", "name", m.GetName(), "value", m.GetValue(), "labels", m.GetLabels())

	col, code, err := r.lookupCollector(m.GetName())
	if err != nil {
		return nil, connect.NewError(code, err)
	}

	switch c := col.(type) {
	case prometheus.Gauge:
		c.Set(m.GetValue())
	case *prometheus.GaugeVec:
		gv, err := vecMetric(r, c, m)
		if err != nil {
			return nil, err
		}
		gv.Set(m.GetValue())
	default:
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("%w: %s does not support Set", errUnsupportedOpForCol, m.GetName()))
	}

	r.log.Debug("set operation finished successfully", "name", m.GetName(), "labels", m.GetLabels(), "value", m.GetValue())
	return connect.NewResponse(&metricsV1.Response{Ok: true}), nil
}

func (r *rpc) Declare(_ context.Context, req *connect.Request[metricsV1.DeclareRequest]) (*connect.Response[metricsV1.Response], error) {
	const op = errors.Op("metrics_rpc_declare")

	nc := req.Msg.GetCollector()
	r.p.mu.Lock()
	defer r.p.mu.Unlock()

	r.log.Debug("declaring new metric", "name", nc.GetName(), "type", nc.GetCollector().GetType(), "namespace", nc.GetCollector().GetNamespace())
	if _, exist := r.p.collectors.Load(nc.GetName()); exist {
		r.log.Warn("metric with provided name already exist", "name", nc.GetName())
		return connect.NewResponse(&metricsV1.Response{Ok: true}), nil
	}

	promCol, err := buildPromCollector(nc)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.E(op, err))
	}

	if err := r.p.Register(promCol); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.E(op, err))
	}

	r.p.collectors.Store(nc.GetName(), &collector{col: promCol, registered: true})
	r.log.Debug("metric successfully added", "name", nc.GetName(), "type", nc.GetCollector().GetType(), "namespace", nc.GetCollector().GetNamespace())
	return connect.NewResponse(&metricsV1.Response{Ok: true}), nil
}

func (r *rpc) Unregister(_ context.Context, req *connect.Request[metricsV1.UnregisterRequest]) (*connect.Response[metricsV1.Response], error) {
	name := req.Msg.GetName()
	r.log.Debug("unregistering collector", "name", name)

	c, exist := r.p.collectors.LoadAndDelete(name)
	if !exist || c == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("%w: %s", errUndefinedCollector, name))
	}

	col, ok := c.(*collector)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("collectors map held non-*collector for %s", name))
	}
	if r.p.registry.Unregister(col.col) {
		r.log.Debug("collector was successfully unregistered", "name", name)
		return connect.NewResponse(&metricsV1.Response{Ok: true}), nil
	}
	// Preserves legacy contract: prometheus refused to unregister (already
	// gone, or never registered there). The collector is removed from our map
	// either way, but the caller deserves to know prometheus state diverged.
	r.log.Debug("collector was deleted from the RR registry but not from the prometheus collector", "name", name)
	return connect.NewResponse(&metricsV1.Response{Ok: false}), nil
}

func (r *rpc) lookupCollector(name string) (prometheus.Collector, connect.Code, error) {
	c, exist := r.p.collectors.Load(name)
	if !exist || c == nil {
		r.log.Error("undefined collector", "collector", name)
		return nil, connect.CodeNotFound, fmt.Errorf("%w: %s", errUndefinedCollector, name)
	}
	col, ok := c.(*collector)
	if !ok {
		return nil, connect.CodeInternal, fmt.Errorf("collectors map held non-*collector for %s", name)
	}
	return col.col, 0, nil
}

func vecMetric[V any, T interface {
	GetMetricWithLabelValues(lvs ...string) (V, error)
}](r *rpc, c T, m *metricsV1.Metric) (V, error) {
	var zero V
	if len(m.GetLabels()) == 0 {
		r.log.Error("required labels for collector", "collector", m.GetName())
		return zero, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("%w: %s", errRequiredLabels, m.GetName()))
	}
	v, err := c.GetMetricWithLabelValues(m.GetLabels()...)
	if err != nil {
		r.log.Error("failed to get metrics with label values", "collector", m.GetName(), "labels", m.GetLabels())
		return zero, connect.NewError(connect.CodeInvalidArgument, err)
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
		objectives := make(map[float64]float64, len(col.GetObjectives()))
		for _, o := range col.GetObjectives() {
			objectives[o.GetQuantile()] = o.GetError()
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
