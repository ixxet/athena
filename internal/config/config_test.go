package config

import (
	"strings"
	"testing"
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
