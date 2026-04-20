package edgehistory

import "time"

type AnalyticsFilter struct {
	FacilityID   string
	ZoneID       string
	NodeID       string
	Since        time.Time
	Until        time.Time
	BucketSize   time.Duration
	SessionLimit int
}

type AnalyticsReport struct {
	FacilityID         string             `json:"facility_id"`
	ZoneID             string             `json:"zone_id,omitempty"`
	NodeID             string             `json:"node_id,omitempty"`
	Since              time.Time          `json:"since"`
	Until              time.Time          `json:"until"`
	BucketMinutes      int                `json:"bucket_minutes"`
	ObservationSummary ObservationSummary `json:"observation_summary"`
	SessionSummary     SessionSummary     `json:"session_summary"`
	FlowBuckets        []FlowBucket       `json:"flow_buckets"`
	NodeBreakdown      []NodeBreakdown    `json:"node_breakdown"`
	Sessions           []SessionFact      `json:"sessions"`
}

type ObservationSummary struct {
	Total                 int `json:"total"`
	Pass                  int `json:"pass"`
	Fail                  int `json:"fail"`
	CommittedPass         int `json:"committed_pass"`
	Accepted              int `json:"accepted"`
	AcceptedTouchnetPass  int `json:"accepted_touchnet_pass"`
	AcceptedTestingPolicy int `json:"accepted_testing_policy"`
	RecognizedDenied      int `json:"recognized_denied"`
	BadAccountNumber      int `json:"bad_account_number"`
	UnclassifiedFail      int `json:"unclassified_fail"`
}

type SessionSummary struct {
	OpenCount              int   `json:"open_count"`
	ClosedCount            int   `json:"closed_count"`
	UnmatchedExitCount     int   `json:"unmatched_exit_count"`
	UniqueVisitors         int   `json:"unique_visitors"`
	AverageDurationSeconds int64 `json:"average_duration_seconds"`
	MedianDurationSeconds  int64 `json:"median_duration_seconds"`
	OccupancyAtEnd         int   `json:"occupancy_at_end"`
}

type FlowBucket struct {
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	PassIn       int       `json:"pass_in"`
	PassOut      int       `json:"pass_out"`
	FailIn       int       `json:"fail_in"`
	FailOut      int       `json:"fail_out"`
	OccupancyEnd int       `json:"occupancy_end"`
}

type NodeBreakdown struct {
	NodeID                string `json:"node_id"`
	Total                 int    `json:"total"`
	Pass                  int    `json:"pass"`
	Fail                  int    `json:"fail"`
	CommittedPass         int    `json:"committed_pass"`
	Accepted              int    `json:"accepted"`
	AcceptedTestingPolicy int    `json:"accepted_testing_policy"`
}

type SessionFact struct {
	SessionID       string     `json:"session_id"`
	State           string     `json:"state"`
	EntryEventID    string     `json:"entry_event_id,omitempty"`
	EntryNodeID     string     `json:"entry_node_id,omitempty"`
	EntryAt         *time.Time `json:"entry_at,omitempty"`
	ExitEventID     string     `json:"exit_event_id,omitempty"`
	ExitNodeID      string     `json:"exit_node_id,omitempty"`
	ExitAt          *time.Time `json:"exit_at,omitempty"`
	DurationSeconds *int64     `json:"duration_seconds,omitempty"`
}
