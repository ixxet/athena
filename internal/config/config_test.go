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
