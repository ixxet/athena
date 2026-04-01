package metrics

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	registry         *prometheus.Registry
	currentOccupancy prometheus.Gauge
}

func New() *Metrics {
	registry := prometheus.NewRegistry()
	currentOccupancy := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "athena_current_occupancy",
		Help: "Current occupancy count produced by ATHENA.",
	})

	registry.MustRegister(currentOccupancy)

	return &Metrics{
		registry:         registry,
		currentOccupancy: currentOccupancy,
	}
}

func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

func (m *Metrics) SetCurrentOccupancy(count int) {
	m.currentOccupancy.Set(float64(count))
}
