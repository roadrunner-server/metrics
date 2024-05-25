package metrics

import (
	"bytes"
	"testing"

	"github.com/goccy/go-json"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func Test_Config_Hydrate_Error1(t *testing.T) {
	cfg := `{"request": {"From": "Something"}}`
	c := &Config{}
	f := new(bytes.Buffer)
	f.WriteString(cfg)

	err := json.Unmarshal(f.Bytes(), c)
	if err != nil {
		t.Fatal(err)
	}
}

func Test_Config_Hydrate_Error2(t *testing.T) {
	cfg := `{"dir": "/dir/"`
	c := &Config{}

	f := new(bytes.Buffer)
	f.WriteString(cfg)

	err := json.Unmarshal(f.Bytes(), c)
	assert.Error(t, err)
}

func Test_Config_Metrics(t *testing.T) {
	cfg := `{
"collect":{
	"metric1":{"type": "gauge"},
	"metric2":{	"type": "counter"},
	"metric3":{"type": "summary"},
	"metric4":{"type": "histogram"}
}
}`
	c := &Config{}
	f := new(bytes.Buffer)
	f.WriteString(cfg)

	err := json.Unmarshal(f.Bytes(), c)
	if err != nil {
		t.Fatal(err)
	}

	m, err := c.getCollectors()
	assert.NoError(t, err)

	assert.IsType(t, prometheus.NewGauge(prometheus.GaugeOpts{}), m["metric1"].col)
	assert.IsType(t, prometheus.NewCounter(prometheus.CounterOpts{}), m["metric2"].col)
	assert.IsType(t, prometheus.NewSummary(prometheus.SummaryOpts{}), m["metric3"].col)
	assert.IsType(t, prometheus.NewHistogram(prometheus.HistogramOpts{}), m["metric4"].col)
}

func Test_Config_MetricsVector(t *testing.T) {
	cfg := `{
"collect":{
	"metric1":{"type": "gauge","labels":["label"]},
	"metric2":{	"type": "counter","labels":["label"]},
	"metric3":{"type": "summary","labels":["label"]},
	"metric4":{"type": "histogram","labels":["label"]}
}
}`
	c := &Config{}
	f := new(bytes.Buffer)
	f.WriteString(cfg)

	err := json.Unmarshal(f.Bytes(), c)
	if err != nil {
		t.Fatal(err)
	}

	m, err := c.getCollectors()
	assert.NoError(t, err)

	assert.IsType(t, prometheus.NewGaugeVec(prometheus.GaugeOpts{}, []string{}), m["metric1"].col)
	assert.IsType(t, prometheus.NewCounterVec(prometheus.CounterOpts{}, []string{}), m["metric2"].col)
	assert.IsType(t, prometheus.NewSummaryVec(prometheus.SummaryOpts{}, []string{}), m["metric3"].col)
	assert.IsType(t, prometheus.NewHistogramVec(prometheus.HistogramOpts{}, []string{}), m["metric4"].col)
}

func Test_Config_Hydrate(t *testing.T) {
	cfg := `{
"address": "127.0.0.1:2112",
"labels": {"app":"testapp", "env":"testenv"},
"collect":{
	"metric1":{
		"namespace": "metric1_namespace",
		"subsystem": "metric1_subsystem",
		"type": "gauge",
		"help":"metric1_help",
		"labels":["label1_metric1", "label2_metric1"],
		"buckets": [0.1, 0.01]
}
}
}`
	c := &Config{}
	f := new(bytes.Buffer)
	f.WriteString(cfg)

	err := json.Unmarshal(f.Bytes(), c)
	if err != nil {
		t.Fatal(err)
	}
	wantConfig := Config{
		Address: "127.0.0.1:2112",
		Labels:  map[string]string{"app": "testapp", "env": "testenv"},
		Collect: map[string]Collector{
			"metric1": {
				Namespace: "metric1_namespace",
				Subsystem: "metric1_subsystem",
				Type:      "gauge",
				Help:      "metric1_help",
				Labels:    []string{"label1_metric1", "label2_metric1"},
				Buckets:   []float64{0.1, 0.01},
			},
		},
	}

	assert.Equal(t, wantConfig, *c)
}
