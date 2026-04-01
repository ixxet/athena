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
