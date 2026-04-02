package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	protoevents "github.com/ixxet/ashton-proto/events"
	"github.com/ixxet/athena/internal/config"
	"github.com/ixxet/athena/internal/publish"
)

type occupancyOutput struct {
	FacilityID   string `json:"facility_id"`
	CurrentCount int    `json:"current_count"`
}

type publishOutput struct {
	Subject        string `json:"subject"`
	PublishedCount int    `json:"published_count"`
}

type recordingPublisher struct {
	subjects []string
	payloads [][]byte
}

func (p *recordingPublisher) Publish(_ context.Context, subject string, payload []byte) error {
	p.subjects = append(p.subjects, subject)
	p.payloads = append(p.payloads, payload)
	return nil
}

func executeRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return out.String(), err
}

func TestPresenceCountDefaultTextOutput(t *testing.T) {
	t.Setenv("ATHENA_ADAPTER", "mock")
	t.Setenv("ATHENA_MOCK_FACILITY_ID", "ashtonbee")
	t.Setenv("ATHENA_MOCK_ENTRIES", "4")
	t.Setenv("ATHENA_MOCK_EXITS", "1")

	output, err := executeRoot(t, "presence", "count")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(output, "facility=ashtonbee") {
		t.Fatalf("output = %q, want facility=ashtonbee", output)
	}
	if !strings.Contains(output, "current_count=3") {
		t.Fatalf("output = %q, want current_count=3", output)
	}
}

func TestPresenceCountJSONOutput(t *testing.T) {
	t.Setenv("ATHENA_ADAPTER", "mock")
	t.Setenv("ATHENA_MOCK_FACILITY_ID", "ashtonbee")
	t.Setenv("ATHENA_MOCK_ENTRIES", "5")
	t.Setenv("ATHENA_MOCK_EXITS", "2")

	output, err := executeRoot(t, "presence", "count", "--format", "json")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var snapshot occupancyOutput
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if snapshot.FacilityID != "ashtonbee" {
		t.Fatalf("facility_id = %q, want %q", snapshot.FacilityID, "ashtonbee")
	}
	if snapshot.CurrentCount != 3 {
		t.Fatalf("current_count = %d, want 3", snapshot.CurrentCount)
	}
}

func TestPresenceCountUnknownFacilityMatchesHTTPSemantics(t *testing.T) {
	t.Setenv("ATHENA_ADAPTER", "mock")
	t.Setenv("ATHENA_MOCK_FACILITY_ID", "ashtonbee")
	t.Setenv("ATHENA_MOCK_ENTRIES", "5")
	t.Setenv("ATHENA_MOCK_EXITS", "2")

	output, err := executeRoot(t, "presence", "count", "--facility", "missing", "--format", "json")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var snapshot occupancyOutput
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if snapshot.FacilityID != "missing" {
		t.Fatalf("facility_id = %q, want %q", snapshot.FacilityID, "missing")
	}
	if snapshot.CurrentCount != 0 {
		t.Fatalf("current_count = %d, want 0", snapshot.CurrentCount)
	}
}

func TestPresencePublishIdentifiedJSONOutput(t *testing.T) {
	t.Setenv("ATHENA_ADAPTER", "mock")
	t.Setenv("ATHENA_NATS_URL", "nats://example")
	t.Setenv("ATHENA_MOCK_FACILITY_ID", "ashtonbee")
	t.Setenv("ATHENA_MOCK_ENTRIES", "2")
	t.Setenv("ATHENA_MOCK_EXITS", "0")
	t.Setenv("ATHENA_MOCK_IDENTIFIED_TAG_HASHES", "tag_tracer2_001")

	publisher := &recordingPublisher{}
	previousFactory := newPublisherHandle
	newPublisherHandle = func(cfg config.Config) (publish.Publisher, func() error, error) {
		return publisher, func() error { return nil }, nil
	}
	defer func() {
		newPublisherHandle = previousFactory
	}()

	output, err := executeRoot(t, "presence", "publish-identified", "--format", "json")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var response publishOutput
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if response.Subject != protoevents.SubjectIdentifiedPresenceArrived {
		t.Fatalf("response.Subject = %q, want %q", response.Subject, protoevents.SubjectIdentifiedPresenceArrived)
	}
	if response.PublishedCount != 1 {
		t.Fatalf("response.PublishedCount = %d, want 1", response.PublishedCount)
	}
	if len(publisher.subjects) != 1 {
		t.Fatalf("len(publisher.subjects) = %d, want 1", len(publisher.subjects))
	}
	if publisher.subjects[0] != protoevents.SubjectIdentifiedPresenceArrived {
		t.Fatalf("publisher.subjects[0] = %q, want %q", publisher.subjects[0], protoevents.SubjectIdentifiedPresenceArrived)
	}
	if !bytes.Contains(publisher.payloads[0], []byte(`"external_identity_hash":"tag_tracer2_001"`)) {
		t.Fatalf("publisher.payloads[0] = %q, want tag_tracer2_001", publisher.payloads[0])
	}
}
