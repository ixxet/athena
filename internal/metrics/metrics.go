package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ixxet/athena/internal/domain"
)

type OccupancyReader interface {
	DefaultOccupancy(context.Context) (domain.OccupancyState, error)
}

type Metrics struct {
	registry *prometheus.Registry
}

func New(reader OccupancyReader) *Metrics {
	registry := prometheus.NewRegistry()
	currentOccupancy := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "athena_current_occupancy",
		Help: "Current occupancy count produced by ATHENA.",
	}, func() float64 {
		snapshot, err := reader.DefaultOccupancy(context.Background())
		if err != nil {
			return 0
		}

		return float64(snapshot.CurrentCount)
	})

	registry.MustRegister(currentOccupancy)

	return &Metrics{
		registry: registry,
	}
}

func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}
