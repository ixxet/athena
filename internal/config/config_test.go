package config

import (
	"strings"
	"testing"
	"time"

	"github.com/ixxet/athena/internal/presence"
)

func TestLoadRejectsInvalidAdapter(t *testing.T) {
	t.Setenv("ATHENA_ADAPTER", "invalid")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid adapter error")
	}
	if !strings.Contains(err.Error(), "ATHENA_ADAPTER") {
		t.Fatalf("Load() error = %q, want ATHENA_ADAPTER context", err)
	}
}

func TestLoadRejectsInvalidMockCount(t *testing.T) {
	t.Setenv("ATHENA_MOCK_ENTRIES", "nope")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid mock count error")
	}
	if !strings.Contains(err.Error(), "ATHENA_MOCK_ENTRIES") {
		t.Fatalf("Load() error = %q, want ATHENA_MOCK_ENTRIES context", err)
	}
}

func TestLoadRejectsInvalidPublishInterval(t *testing.T) {
	t.Setenv("ATHENA_IDENTIFIED_PUBLISH_INTERVAL", "soon")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid publish interval error")
	}
	if !strings.Contains(err.Error(), "ATHENA_IDENTIFIED_PUBLISH_INTERVAL") {
		t.Fatalf("Load() error = %q, want ATHENA_IDENTIFIED_PUBLISH_INTERVAL context", err)
	}
}

func TestLoadRejectsInvalidEdgeOccupancyProjectionFlag(t *testing.T) {
	t.Setenv("ATHENA_EDGE_OCCUPANCY_PROJECTION", "soon")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid edge occupancy projection error")
	}
	if !strings.Contains(err.Error(), "ATHENA_EDGE_OCCUPANCY_PROJECTION") {
		t.Fatalf("Load() error = %q, want ATHENA_EDGE_OCCUPANCY_PROJECTION context", err)
	}
}

func TestLoadRejectsInvalidEdgeAnalyticsMaxWindow(t *testing.T) {
	t.Setenv("ATHENA_EDGE_ANALYTICS_MAX_WINDOW", "soon")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid analytics max window error")
	}
	if !strings.Contains(err.Error(), "ATHENA_EDGE_ANALYTICS_MAX_WINDOW") {
		t.Fatalf("Load() error = %q, want ATHENA_EDGE_ANALYTICS_MAX_WINDOW context", err)
	}
}

func TestLoadDefaultsProjectorBounds(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.EdgeProjectorAbsentRetention != presence.DefaultAbsentIdentityRetention {
		t.Fatalf("EdgeProjectorAbsentRetention = %s, want %s", cfg.EdgeProjectorAbsentRetention, presence.DefaultAbsentIdentityRetention)
	}
	if cfg.EdgeProjectorMaxAbsentIdentities != presence.DefaultMaxAbsentIdentities {
		t.Fatalf("EdgeProjectorMaxAbsentIdentities = %d, want %d", cfg.EdgeProjectorMaxAbsentIdentities, presence.DefaultMaxAbsentIdentities)
	}
}

func TestLoadRejectsInvalidEdgeProjectorAbsentRetention(t *testing.T) {
	t.Setenv("ATHENA_EDGE_PROJECTOR_ABSENT_RETENTION", "0s")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid projector absent retention error")
	}
	if !strings.Contains(err.Error(), "ATHENA_EDGE_PROJECTOR_ABSENT_RETENTION") {
		t.Fatalf("Load() error = %q, want ATHENA_EDGE_PROJECTOR_ABSENT_RETENTION context", err)
	}
}

func TestLoadRejectsInvalidEdgeProjectorMaxAbsentIdentities(t *testing.T) {
	t.Setenv("ATHENA_EDGE_PROJECTOR_MAX_ABSENT_IDENTITIES", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid projector max absent identities error")
	}
	if !strings.Contains(err.Error(), "ATHENA_EDGE_PROJECTOR_MAX_ABSENT_IDENTITIES") {
		t.Fatalf("Load() error = %q, want ATHENA_EDGE_PROJECTOR_MAX_ABSENT_IDENTITIES context", err)
	}
}

func TestLoadParsesMockIdentifiedTagHashes(t *testing.T) {
	t.Setenv("ATHENA_MOCK_IDENTIFIED_TAG_HASHES", " tag-1, ,tag-2 ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.MockIdentifiedTagHashes) != 2 {
		t.Fatalf("len(MockIdentifiedTagHashes) = %d, want 2", len(cfg.MockIdentifiedTagHashes))
	}
	if cfg.MockIdentifiedTagHashes[0] != "tag-1" || cfg.MockIdentifiedTagHashes[1] != "tag-2" {
		t.Fatalf("MockIdentifiedTagHashes = %#v, want [tag-1 tag-2]", cfg.MockIdentifiedTagHashes)
	}
}

func TestLoadRejectsCSVAdapterWithoutSourcePath(t *testing.T) {
	t.Setenv("ATHENA_ADAPTER", "csv")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing csv path error")
	}
	if !strings.Contains(err.Error(), "ATHENA_CSV_PATH") {
		t.Fatalf("Load() error = %q, want ATHENA_CSV_PATH context", err)
	}
}

func TestLoadParsesCSVAdapterConfig(t *testing.T) {
	t.Setenv("ATHENA_ADAPTER", "csv")
	t.Setenv("ATHENA_CSV_PATH", "/tmp/source.csv")
	t.Setenv("ATHENA_DEFAULT_FACILITY_ID", "source-facility")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Adapter != "csv" {
		t.Fatalf("Adapter = %q, want csv", cfg.Adapter)
	}
	if cfg.CSVPath != "/tmp/source.csv" {
		t.Fatalf("CSVPath = %q, want /tmp/source.csv", cfg.CSVPath)
	}
	if cfg.DefaultFacilityID != "source-facility" {
		t.Fatalf("DefaultFacilityID = %q, want source-facility", cfg.DefaultFacilityID)
	}
}

func TestLoadRejectsEdgeTokensWithoutHashSalt(t *testing.T) {
	t.Setenv("ATHENA_EDGE_TOKENS", "entry=node-token")
	t.Setenv("ATHENA_NATS_URL", "nats://example:4222")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing hash salt error")
	}
	if !strings.Contains(err.Error(), "ATHENA_EDGE_HASH_SALT") {
		t.Fatalf("Load() error = %q, want ATHENA_EDGE_HASH_SALT context", err)
	}
}

func TestLoadRejectsEdgeHashSaltWithoutTokens(t *testing.T) {
	t.Setenv("ATHENA_EDGE_HASH_SALT", "salt")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing edge tokens error")
	}
	if !strings.Contains(err.Error(), "ATHENA_EDGE_TOKENS") {
		t.Fatalf("Load() error = %q, want ATHENA_EDGE_TOKENS context", err)
	}
}

func TestLoadRejectsEdgeIngressWithoutNATS(t *testing.T) {
	t.Setenv("ATHENA_EDGE_HASH_SALT", "salt")
	t.Setenv("ATHENA_EDGE_TOKENS", "entry=node-token")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing NATS error")
	}
	if !strings.Contains(err.Error(), "ATHENA_NATS_URL") {
		t.Fatalf("Load() error = %q, want ATHENA_NATS_URL context", err)
	}
}

func TestLoadParsesEdgeTokens(t *testing.T) {
	t.Setenv("ATHENA_NATS_URL", "nats://example:4222")
	t.Setenv("ATHENA_EDGE_HASH_SALT", "salt")
	t.Setenv("ATHENA_EDGE_TOKENS", "entry = node-token , exit=other-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.EdgeHashSalt != "salt" {
		t.Fatalf("EdgeHashSalt = %q, want salt", cfg.EdgeHashSalt)
	}
	if len(cfg.EdgeTokens) != 2 {
		t.Fatalf("len(EdgeTokens) = %d, want 2", len(cfg.EdgeTokens))
	}
	if cfg.EdgeTokens["entry"] != "node-token" {
		t.Fatalf("EdgeTokens[entry] = %q, want node-token", cfg.EdgeTokens["entry"])
	}
	if cfg.EdgeTokens["exit"] != "other-token" {
		t.Fatalf("EdgeTokens[exit] = %q, want other-token", cfg.EdgeTokens["exit"])
	}
}

func TestLoadRejectsEdgeProjectionWithoutIngress(t *testing.T) {
	t.Setenv("ATHENA_EDGE_OCCUPANCY_PROJECTION", "true")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing edge ingress error")
	}
	if !strings.Contains(err.Error(), "ATHENA_EDGE_OCCUPANCY_PROJECTION") {
		t.Fatalf("Load() error = %q, want ATHENA_EDGE_OCCUPANCY_PROJECTION context", err)
	}
}

func TestLoadParsesEdgeProjectionConfig(t *testing.T) {
	t.Setenv("ATHENA_NATS_URL", "nats://example:4222")
	t.Setenv("ATHENA_EDGE_HASH_SALT", "salt")
	t.Setenv("ATHENA_EDGE_TOKENS", "entry=node-token")
	t.Setenv("ATHENA_EDGE_OCCUPANCY_PROJECTION", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.EdgeOccupancyProjection {
		t.Fatal("EdgeOccupancyProjection = false, want true")
	}
}

func TestLoadParsesEdgeObservationHistoryPath(t *testing.T) {
	t.Setenv("ATHENA_EDGE_OBSERVATION_HISTORY_PATH", "/tmp/athena-edge-history.jsonl")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.EdgeObservationHistoryPath != "/tmp/athena-edge-history.jsonl" {
		t.Fatalf("EdgeObservationHistoryPath = %q, want /tmp/athena-edge-history.jsonl", cfg.EdgeObservationHistoryPath)
	}
}

func TestLoadParsesEdgePostgresConfig(t *testing.T) {
	t.Setenv("ATHENA_EDGE_POSTGRES_DSN", "postgres://athena:secret@127.0.0.1:5432/athena?sslmode=disable")
	t.Setenv("ATHENA_EDGE_ANALYTICS_MAX_WINDOW", "96h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.EdgePostgresDSN != "postgres://athena:secret@127.0.0.1:5432/athena?sslmode=disable" {
		t.Fatalf("EdgePostgresDSN = %q, want configured dsn", cfg.EdgePostgresDSN)
	}
	if cfg.EdgeAnalyticsMaxWindow != 96*time.Hour {
		t.Fatalf("EdgeAnalyticsMaxWindow = %s, want 96h", cfg.EdgeAnalyticsMaxWindow)
	}
}

func TestLoadRejectsMixedEdgeHistoryBackends(t *testing.T) {
	t.Setenv("ATHENA_EDGE_POSTGRES_DSN", "postgres://athena:secret@127.0.0.1:5432/athena?sslmode=disable")
	t.Setenv("ATHENA_EDGE_OBSERVATION_HISTORY_PATH", "/tmp/athena-edge-history.jsonl")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want mixed backend error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("Load() error = %q, want mutually exclusive context", err)
	}
}

func TestLoadParsesFacilityCatalogPath(t *testing.T) {
	t.Setenv("ATHENA_FACILITY_CATALOG_PATH", "/tmp/athena-facilities.json")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.FacilityCatalogPath != "/tmp/athena-facilities.json" {
		t.Fatalf("FacilityCatalogPath = %q, want /tmp/athena-facilities.json", cfg.FacilityCatalogPath)
	}
}
