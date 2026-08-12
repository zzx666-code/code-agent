package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type promRegistry struct {
	reg    *prometheus.Registry
	mu     sync.Mutex
	cached map[string]any
}

func NewPrometheusRegistry() Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	return &promRegistry{
		reg:    reg,
		cached: make(map[string]any),
	}
}

func (p *promRegistry) Counter(name, help string, labels ...string) Counter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.cached[name].(*prometheus.CounterVec); ok {
		return &promCounter{cv: c}
	}
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: name,
		Help: help,
	}, labels)
	p.reg.MustRegister(cv)
	p.cached[name] = cv
	return &promCounter{cv: cv}
}

func (p *promRegistry) Histogram(name, help string, buckets []float64, labels ...string) Histogram {
	p.mu.Lock()
	defer p.mu.Unlock()
	if h, ok := p.cached[name].(*prometheus.HistogramVec); ok {
		return &promHistogram{hv: h}
	}
	hv := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    name,
		Help:    help,
		Buckets: buckets,
	}, labels)
	p.reg.MustRegister(hv)
	p.cached[name] = hv
	return &promHistogram{hv: hv}
}

func (p *promRegistry) Gauge(name, help string, labels ...string) Gauge {
	p.mu.Lock()
	defer p.mu.Unlock()
	if g, ok := p.cached[name].(*prometheus.GaugeVec); ok {
		return &promGauge{gv: g}
	}
	gv := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: name,
		Help: help,
	}, labels)
	p.reg.MustRegister(gv)
	p.cached[name] = gv
	return &promGauge{gv: gv}
}

func (p *promRegistry) Handler() http.Handler {
	return promhttp.HandlerFor(p.reg, promhttp.HandlerOpts{
		Registry: p.reg,
	})
}

func (p *promRegistry) Enabled() bool { return true }

type promCounter struct {
	cv *prometheus.CounterVec
}

func (c *promCounter) Inc(labelValues ...string) {
	c.cv.WithLabelValues(labelValues...).Inc()
}

func (c *promCounter) Add(v float64, labelValues ...string) {
	c.cv.WithLabelValues(labelValues...).Add(v)
}

type promHistogram struct {
	hv *prometheus.HistogramVec
}

func (h *promHistogram) Observe(v float64, labelValues ...string) {
	h.hv.WithLabelValues(labelValues...).Observe(v)
}

type promGauge struct {
	gv *prometheus.GaugeVec
}

func (g *promGauge) Set(v float64, labelValues ...string) {
	g.gv.WithLabelValues(labelValues...).Set(v)
}

func (g *promGauge) Inc(labelValues ...string) {
	g.gv.WithLabelValues(labelValues...).Inc()
}

func (g *promGauge) Dec(labelValues ...string) {
	g.gv.WithLabelValues(labelValues...).Dec()
}
