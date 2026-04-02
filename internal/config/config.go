package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                    string
	Adapter                     string
	NATSURL                     string
	IdentifiedPublishInterval   time.Duration
	MockFacilityID              string
	MockZoneID                  string
	MockEntries                 int
	MockExits                   int
	MockIdentifiedTagHashes     []string
	MockIdentifiedExitTagHashes []string
}

func Load() (Config, error) {
	entries, err := getEnvAsInt("ATHENA_MOCK_ENTRIES", 12)
	if err != nil {
		return Config{}, err
	}

	exits, err := getEnvAsInt("ATHENA_MOCK_EXITS", 3)
	if err != nil {
		return Config{}, err
	}

	interval, err := getEnvAsDuration("ATHENA_IDENTIFIED_PUBLISH_INTERVAL", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr:                    getEnv("ATHENA_HTTP_ADDR", ":8080"),
		Adapter:                     getEnv("ATHENA_ADAPTER", "mock"),
		NATSURL:                     getEnv("ATHENA_NATS_URL", ""),
		IdentifiedPublishInterval:   interval,
		MockFacilityID:              getEnv("ATHENA_MOCK_FACILITY_ID", "ashtonbee"),
		MockZoneID:                  getEnv("ATHENA_MOCK_ZONE_ID", ""),
		MockEntries:                 entries,
		MockExits:                   exits,
		MockIdentifiedTagHashes:     splitCSV(getEnv("ATHENA_MOCK_IDENTIFIED_TAG_HASHES", "")),
		MockIdentifiedExitTagHashes: splitCSV(getEnv("ATHENA_MOCK_IDENTIFIED_EXIT_TAG_HASHES", "")),
	}

	if cfg.MockEntries < 0 {
		return Config{}, fmt.Errorf("invalid ATHENA_MOCK_ENTRIES %d: value must be >= 0", cfg.MockEntries)
	}
	if cfg.MockExits < 0 {
		return Config{}, fmt.Errorf("invalid ATHENA_MOCK_EXITS %d: value must be >= 0", cfg.MockExits)
	}
	if cfg.IdentifiedPublishInterval <= 0 {
		return Config{}, fmt.Errorf("invalid ATHENA_IDENTIFIED_PUBLISH_INTERVAL %s: value must be > 0", cfg.IdentifiedPublishInterval)
	}

	switch cfg.Adapter {
	case "mock":
		return cfg, nil
	default:
		return Config{}, fmt.Errorf("invalid ATHENA_ADAPTER %q: supported values: mock", cfg.Adapter)
	}
}

func getEnv(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback
	}

	return value
}

func getEnvAsInt(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, value, err)
	}

	return parsed, nil
}

func getEnvAsDuration(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, value, err)
	}

	return parsed, nil
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}

	return out
}
