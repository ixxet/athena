package presence

import (
	"context"

	"github.com/ixxet/athena/internal/domain"
)

type OccupancyService interface {
	CurrentOccupancy(context.Context, domain.OccupancyFilter) (domain.OccupancyState, error)
}

type ReadPath struct {
	service       OccupancyService
	defaultFilter domain.OccupancyFilter
}

func NewReadPath(service OccupancyService, defaultFilter domain.OccupancyFilter) *ReadPath {
	return &ReadPath{
		service:       service,
		defaultFilter: defaultFilter,
	}
}

func (r *ReadPath) CurrentOccupancy(ctx context.Context, filter domain.OccupancyFilter) (domain.OccupancyState, error) {
	return r.service.CurrentOccupancy(ctx, r.resolveFilter(filter))
}

func (r *ReadPath) DefaultOccupancy(ctx context.Context) (domain.OccupancyState, error) {
	return r.CurrentOccupancy(ctx, domain.OccupancyFilter{})
}

func (r *ReadPath) DefaultOccupancySnapshot() (domain.OccupancyState, error) {
	return r.service.CurrentOccupancy(context.Background(), r.resolveFilter(domain.OccupancyFilter{}))
}

func (r *ReadPath) DefaultFilter() domain.OccupancyFilter {
	return r.defaultFilter
}

func (r *ReadPath) resolveFilter(filter domain.OccupancyFilter) domain.OccupancyFilter {
	resolved := filter
	if resolved.FacilityID == "" {
		resolved.FacilityID = r.defaultFilter.FacilityID
		if resolved.ZoneID == "" {
			resolved.ZoneID = r.defaultFilter.ZoneID
		}
	}

	return resolved
}
