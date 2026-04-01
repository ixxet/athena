package config

import (
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

func Load() Config {
	return Config{
		HTTPAddr:       getEnv("ATHENA_HTTP_ADDR", ":8080"),
		Adapter:        getEnv("ATHENA_ADAPTER", "mock"),
		MockFacilityID: getEnv("ATHENA_MOCK_FACILITY_ID", "ashtonbee"),
		MockZoneID:     getEnv("ATHENA_MOCK_ZONE_ID", ""),
		MockEntries:    getEnvAsInt("ATHENA_MOCK_ENTRIES", 12),
		MockExits:      getEnvAsInt("ATHENA_MOCK_EXITS", 3),
	}
}

func getEnv(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback
	}

	return value
}

func getEnvAsInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}
