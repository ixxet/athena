package facility

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

var ErrCatalogNotConfigured = errors.New("facility catalog is not configured")

type Catalog struct {
	Facilities []Facility `json:"facilities"`
}

type Summary struct {
	FacilityID string `json:"facility_id"`
	Name       string `json:"name"`
	Timezone   string `json:"timezone"`
}

type Facility struct {
	FacilityID     string            `json:"facility_id"`
	Name           string            `json:"name"`
	Timezone       string            `json:"timezone"`
	Hours          []HoursWindow     `json:"hours"`
	Zones          []Zone            `json:"zones"`
	ClosureWindows []ClosureWindow   `json:"closure_windows,omitempty"`
	Metadata       map[string]string `json:"metadata"`
}

type HoursWindow struct {
	Day      string `json:"day"`
	OpensAt  string `json:"opens_at"`
	ClosesAt string `json:"closes_at"`
}

type Zone struct {
	ZoneID string `json:"zone_id"`
	Name   string `json:"name"`
}

type ClosureWindow struct {
	StartsAt string   `json:"starts_at"`
	EndsAt   string   `json:"ends_at"`
	Code     string   `json:"code,omitempty"`
	Reason   string   `json:"reason"`
	ZoneIDs  []string `json:"zone_ids,omitempty"`
}

type Store struct {
	facilities []Facility
	byID       map[string]Facility
}

var weekdayOrder = map[string]int{
	"monday":    0,
	"tuesday":   1,
	"wednesday": 2,
	"thursday":  3,
	"friday":    4,
	"saturday":  5,
	"sunday":    6,
}

func Load(path string) (*Store, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return nil, ErrCatalogNotConfigured
	}

	payload, err := os.ReadFile(trimmedPath)
	if err != nil {
		return nil, fmt.Errorf("read facility catalog: %w", err)
	}

	var catalog Catalog
	if err := json.Unmarshal(payload, &catalog); err != nil {
		return nil, fmt.Errorf("decode facility catalog: %w", err)
	}

	store, err := NewStore(catalog)
	if err != nil {
		return nil, err
	}

	return store, nil
}

func NewStore(catalog Catalog) (*Store, error) {
	if len(catalog.Facilities) == 0 {
		return nil, fmt.Errorf("facility catalog must include at least one facility")
	}

	normalized := make([]Facility, 0, len(catalog.Facilities))
	byID := make(map[string]Facility, len(catalog.Facilities))
	for _, raw := range catalog.Facilities {
		facility, err := normalizeFacility(raw)
		if err != nil {
			return nil, err
		}
		if _, exists := byID[facility.FacilityID]; exists {
			return nil, fmt.Errorf("duplicate facility_id %q", facility.FacilityID)
		}
		byID[facility.FacilityID] = facility
		normalized = append(normalized, facility)
	}

	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].FacilityID < normalized[j].FacilityID
	})

	return &Store{
		facilities: normalized,
		byID:       byID,
	}, nil
}

func (s *Store) List() []Summary {
	if s == nil {
		return nil
	}

	summaries := make([]Summary, 0, len(s.facilities))
	for _, facility := range s.facilities {
		summaries = append(summaries, Summary{
			FacilityID: facility.FacilityID,
			Name:       facility.Name,
			Timezone:   facility.Timezone,
		})
	}

	return summaries
}

func (s *Store) Facility(facilityID string) (Facility, bool) {
	if s == nil {
		return Facility{}, false
	}

	facility, ok := s.byID[strings.TrimSpace(facilityID)]
	if !ok {
		return Facility{}, false
	}

	return cloneFacility(facility), true
}

func normalizeFacility(raw Facility) (Facility, error) {
	facilityID, err := normalizeIdentifier(raw.FacilityID, "facility_id")
	if err != nil {
		return Facility{}, err
	}

	name := strings.TrimSpace(raw.Name)
	if name == "" {
		return Facility{}, fmt.Errorf("facility name for %q is required", facilityID)
	}

	timezone := strings.TrimSpace(raw.Timezone)
	if timezone == "" {
		return Facility{}, fmt.Errorf("facility timezone for %q is required", facilityID)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Facility{}, fmt.Errorf("invalid facility timezone for %q: %w", facilityID, err)
	}

	metadata, err := normalizeMetadata(raw.Metadata, facilityID)
	if err != nil {
		return Facility{}, err
	}

	hours, err := normalizeHours(raw.Hours, facilityID)
	if err != nil {
		return Facility{}, err
	}

	zones, zoneSet, err := normalizeZones(raw.Zones, facilityID)
	if err != nil {
		return Facility{}, err
	}

	closures, err := normalizeClosures(raw.ClosureWindows, facilityID, zoneSet)
	if err != nil {
		return Facility{}, err
	}

	return Facility{
		FacilityID:     facilityID,
		Name:           name,
		Timezone:       timezone,
		Hours:          hours,
		Zones:          zones,
		ClosureWindows: closures,
		Metadata:       metadata,
	}, nil
}

func normalizeMetadata(raw map[string]string, facilityID string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("facility metadata for %q is required", facilityID)
	}

	normalized := make(map[string]string, len(raw))
	for key, value := range raw {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" {
			return nil, fmt.Errorf("facility metadata key for %q is required", facilityID)
		}
		if trimmedValue == "" {
			return nil, fmt.Errorf("facility metadata value for %q key %q is required", facilityID, trimmedKey)
		}
		normalized[trimmedKey] = trimmedValue
	}

	return normalized, nil
}

func normalizeHours(raw []HoursWindow, facilityID string) ([]HoursWindow, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("facility hours for %q are required", facilityID)
	}

	normalized := make([]HoursWindow, 0, len(raw))
	for _, window := range raw {
		day, err := normalizeDay(window.Day)
		if err != nil {
			return nil, fmt.Errorf("invalid hours day for %q: %w", facilityID, err)
		}
		opensAt, openMinutes, err := normalizeClock(window.OpensAt)
		if err != nil {
			return nil, fmt.Errorf("invalid opens_at for %q on %s: %w", facilityID, day, err)
		}
		closesAt, closeMinutes, err := normalizeClock(window.ClosesAt)
		if err != nil {
			return nil, fmt.Errorf("invalid closes_at for %q on %s: %w", facilityID, day, err)
		}
		if openMinutes >= closeMinutes {
			return nil, fmt.Errorf("invalid hours window for %q on %s: opens_at must be before closes_at", facilityID, day)
		}
		normalized = append(normalized, HoursWindow{
			Day:      day,
			OpensAt:  opensAt,
			ClosesAt: closesAt,
		})
	}

	sort.Slice(normalized, func(i, j int) bool {
		left := normalized[i]
		right := normalized[j]
		if left.Day != right.Day {
			return weekdayOrder[left.Day] < weekdayOrder[right.Day]
		}
		if left.OpensAt != right.OpensAt {
			return left.OpensAt < right.OpensAt
		}
		return left.ClosesAt < right.ClosesAt
	})

	byDay := make(map[string][]HoursWindow)
	for _, window := range normalized {
		byDay[window.Day] = append(byDay[window.Day], window)
	}

	for day, windows := range byDay {
		for i := 1; i < len(windows); i++ {
			previous := windows[i-1]
			current := windows[i]
			_, previousClose, _ := normalizeClock(previous.ClosesAt)
			_, currentOpen, _ := normalizeClock(current.OpensAt)
			if currentOpen < previousClose {
				return nil, fmt.Errorf("overlapping hours windows for %q on %s", facilityID, day)
			}
		}
	}

	return normalized, nil
}

func normalizeZones(raw []Zone, facilityID string) ([]Zone, map[string]struct{}, error) {
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("facility zones for %q are required", facilityID)
	}

	normalized := make([]Zone, 0, len(raw))
	zoneSet := make(map[string]struct{}, len(raw))
	for _, zone := range raw {
		zoneID, err := normalizeIdentifier(zone.ZoneID, "zone_id")
		if err != nil {
			return nil, nil, fmt.Errorf("invalid zone for %q: %w", facilityID, err)
		}
		if _, exists := zoneSet[zoneID]; exists {
			return nil, nil, fmt.Errorf("duplicate zone_id %q for facility %q", zoneID, facilityID)
		}
		name := strings.TrimSpace(zone.Name)
		if name == "" {
			return nil, nil, fmt.Errorf("zone name for %q/%q is required", facilityID, zoneID)
		}

		zoneSet[zoneID] = struct{}{}
		normalized = append(normalized, Zone{
			ZoneID: zoneID,
			Name:   name,
		})
	}

	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].ZoneID < normalized[j].ZoneID
	})

	return normalized, zoneSet, nil
}

func normalizeClosures(raw []ClosureWindow, facilityID string, zoneSet map[string]struct{}) ([]ClosureWindow, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	normalized := make([]ClosureWindow, 0, len(raw))
	for _, window := range raw {
		startsAt, err := normalizeTimestamp(window.StartsAt)
		if err != nil {
			return nil, fmt.Errorf("invalid closure start for %q: %w", facilityID, err)
		}
		endsAt, err := normalizeTimestamp(window.EndsAt)
		if err != nil {
			return nil, fmt.Errorf("invalid closure end for %q: %w", facilityID, err)
		}
		if !startsAt.Before(endsAt) {
			return nil, fmt.Errorf("invalid closure window for %q: starts_at must be before ends_at", facilityID)
		}

		reason := strings.TrimSpace(window.Reason)
		if reason == "" {
			return nil, fmt.Errorf("closure reason for %q is required", facilityID)
		}

		zoneIDs := make([]string, 0, len(window.ZoneIDs))
		seen := make(map[string]struct{}, len(window.ZoneIDs))
		for _, zoneID := range window.ZoneIDs {
			normalizedZoneID, err := normalizeIdentifier(zoneID, "zone_id")
			if err != nil {
				return nil, fmt.Errorf("invalid closure zone for %q: %w", facilityID, err)
			}
			if _, ok := zoneSet[normalizedZoneID]; !ok {
				return nil, fmt.Errorf("unknown closure zone %q for facility %q", normalizedZoneID, facilityID)
			}
			if _, exists := seen[normalizedZoneID]; exists {
				return nil, fmt.Errorf("duplicate closure zone %q for facility %q", normalizedZoneID, facilityID)
			}
			seen[normalizedZoneID] = struct{}{}
			zoneIDs = append(zoneIDs, normalizedZoneID)
		}
		sort.Strings(zoneIDs)

		normalized = append(normalized, ClosureWindow{
			StartsAt: startsAt.Format(time.RFC3339),
			EndsAt:   endsAt.Format(time.RFC3339),
			Code:     strings.TrimSpace(window.Code),
			Reason:   reason,
			ZoneIDs:  zoneIDs,
		})
	}

	sort.Slice(normalized, func(i, j int) bool {
		left := normalized[i]
		right := normalized[j]
		if left.StartsAt != right.StartsAt {
			return left.StartsAt < right.StartsAt
		}
		if left.EndsAt != right.EndsAt {
			return left.EndsAt < right.EndsAt
		}
		return left.Reason < right.Reason
	})

	for i := 0; i < len(normalized); i++ {
		leftStart, _ := time.Parse(time.RFC3339, normalized[i].StartsAt)
		leftEnd, _ := time.Parse(time.RFC3339, normalized[i].EndsAt)
		for j := i + 1; j < len(normalized); j++ {
			rightStart, _ := time.Parse(time.RFC3339, normalized[j].StartsAt)
			if !rightStart.Before(leftEnd) {
				break
			}
			rightEnd, _ := time.Parse(time.RFC3339, normalized[j].EndsAt)
			if closuresConflict(normalized[i], leftStart, leftEnd, normalized[j], rightStart, rightEnd) {
				return nil, fmt.Errorf("conflicting closure windows for facility %q", facilityID)
			}
		}
	}

	return normalized, nil
}

func closuresConflict(left ClosureWindow, leftStart, leftEnd time.Time, right ClosureWindow, rightStart, rightEnd time.Time) bool {
	if !leftStart.Before(rightEnd) || !rightStart.Before(leftEnd) {
		return false
	}
	if len(left.ZoneIDs) == 0 || len(right.ZoneIDs) == 0 {
		return true
	}

	rightSet := make(map[string]struct{}, len(right.ZoneIDs))
	for _, zoneID := range right.ZoneIDs {
		rightSet[zoneID] = struct{}{}
	}
	for _, zoneID := range left.ZoneIDs {
		if _, ok := rightSet[zoneID]; ok {
			return true
		}
	}

	return false
}

func normalizeDay(value string) (string, error) {
	day := strings.ToLower(strings.TrimSpace(value))
	if _, ok := weekdayOrder[day]; !ok {
		return "", fmt.Errorf("unsupported day %q", value)
	}
	return day, nil
}

func normalizeClock(value string) (string, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return "", 0, err
	}

	minutes := parsed.Hour()*60 + parsed.Minute()
	return parsed.Format("15:04"), minutes, nil
}

func normalizeTimestamp(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("timestamp is required")
	}

	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, err
	}

	return parsed.UTC(), nil
}

func normalizeIdentifier(value, field string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", field)
	}

	for i, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(trimmed)-1:
		default:
			return "", fmt.Errorf("%s %q must use lowercase letters, digits, and interior hyphens only", field, trimmed)
		}
	}

	return trimmed, nil
}

func cloneFacility(facility Facility) Facility {
	cloned := Facility{
		FacilityID:     facility.FacilityID,
		Name:           facility.Name,
		Timezone:       facility.Timezone,
		Hours:          append([]HoursWindow(nil), facility.Hours...),
		Zones:          append([]Zone(nil), facility.Zones...),
		ClosureWindows: make([]ClosureWindow, 0, len(facility.ClosureWindows)),
		Metadata:       make(map[string]string, len(facility.Metadata)),
	}

	for key, value := range facility.Metadata {
		cloned.Metadata[key] = value
	}
	for _, closure := range facility.ClosureWindows {
		cloned.ClosureWindows = append(cloned.ClosureWindows, ClosureWindow{
			StartsAt: closure.StartsAt,
			EndsAt:   closure.EndsAt,
			Code:     closure.Code,
			Reason:   closure.Reason,
			ZoneIDs:  append([]string(nil), closure.ZoneIDs...),
		})
	}

	return cloned
}
