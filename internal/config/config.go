package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr       string
	Adapter        string
	MockFacilityID string
	MockZoneID     string
	MockEntries    int
	MockExits      int
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

	cfg := Config{
		HTTPAddr:       getEnv("ATHENA_HTTP_ADDR", ":8080"),
		Adapter:        getEnv("ATHENA_ADAPTER", "mock"),
		MockFacilityID: getEnv("ATHENA_MOCK_FACILITY_ID", "ashtonbee"),
		MockZoneID:     getEnv("ATHENA_MOCK_ZONE_ID", ""),
		MockEntries:    entries,
		MockExits:      exits,
	}

	if cfg.MockEntries < 0 {
		return Config{}, fmt.Errorf("invalid ATHENA_MOCK_ENTRIES %d: value must be >= 0", cfg.MockEntries)
	}
	if cfg.MockExits < 0 {
		return Config{}, fmt.Errorf("invalid ATHENA_MOCK_EXITS %d: value must be >= 0", cfg.MockExits)
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
