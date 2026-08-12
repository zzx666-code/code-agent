package metrics

import "net/http"

type noopRegistry struct{}

func (noopRegistry) Counter(name, help string, labels ...string) Counter        { return noopCounter{} }
func (noopRegistry) Histogram(name, help string, buckets []float64, labels ...string) Histogram {
	return noopHistogram{}
}
func (noopRegistry) Gauge(name, help string, labels ...string) Gauge { return noopGauge{} }
func (noopRegistry) Handler() http.Handler                            { return nil }
func (noopRegistry) Enabled() bool                                    { return false }

type noopCounter struct{}

func (noopCounter) Inc(labelValues ...string)                    {}
func (noopCounter) Add(v float64, labelValues ...string)         {}

type noopHistogram struct{}

func (noopHistogram) Observe(v float64, labelValues ...string) {}

type noopGauge struct{}

func (noopGauge) Set(v float64, labelValues ...string)    {}
func (noopGauge) Inc(labelValues ...string)               {}
func (noopGauge) Dec(labelValues ...string)               {}
