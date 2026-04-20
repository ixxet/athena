package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ixxet/athena/internal/presence"
)

type Config struct {
	HTTPAddr                         string
	Adapter                          string
	NATSURL                          string
	IdentifiedPublishInterval        time.Duration
	EdgeHashSalt                     string
	EdgeTokens                       map[string]string
	EdgeOccupancyProjection          bool
	EdgePolicyAcceptanceEnabled      bool
	EdgeProjectorAbsentRetention     time.Duration
	EdgeProjectorMaxAbsentIdentities int
	EdgeObservationHistoryPath       string
	EdgePostgresDSN                  string
	EdgeAnalyticsMaxWindow           time.Duration
	FacilityCatalogPath              string
	DefaultFacilityID                string
	DefaultZoneID                    string
	MockFacilityID                   string
	MockZoneID                       string
	MockEntries                      int
	MockExits                        int
	MockIdentifiedTagHashes          []string
	MockIdentifiedExitTagHashes      []string
	CSVPath                          string
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

	edgeProjection, err := getEnvAsBool("ATHENA_EDGE_OCCUPANCY_PROJECTION", false)
	if err != nil {
		return Config{}, err
	}
	edgePolicyAcceptanceEnabled, err := getEnvAsBool("ATHENA_EDGE_POLICY_ACCEPTANCE_ENABLED", false)
	if err != nil {
		return Config{}, err
	}

	edgeProjectorAbsentRetention, err := getEnvAsDuration("ATHENA_EDGE_PROJECTOR_ABSENT_RETENTION", presence.DefaultAbsentIdentityRetention)
	if err != nil {
		return Config{}, err
	}

	edgeProjectorMaxAbsentIdentities, err := getEnvAsInt("ATHENA_EDGE_PROJECTOR_MAX_ABSENT_IDENTITIES", presence.DefaultMaxAbsentIdentities)
	if err != nil {
		return Config{}, err
	}

	analyticsMaxWindow, err := getEnvAsDuration("ATHENA_EDGE_ANALYTICS_MAX_WINDOW", 7*24*time.Hour)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr:                         getEnv("ATHENA_HTTP_ADDR", ":8080"),
		Adapter:                          getEnv("ATHENA_ADAPTER", "mock"),
		NATSURL:                          getEnv("ATHENA_NATS_URL", ""),
		IdentifiedPublishInterval:        interval,
		EdgeHashSalt:                     getEnv("ATHENA_EDGE_HASH_SALT", ""),
		EdgeTokens:                       parseNodeTokenMap(getEnv("ATHENA_EDGE_TOKENS", "")),
		EdgeOccupancyProjection:          edgeProjection,
		EdgePolicyAcceptanceEnabled:      edgePolicyAcceptanceEnabled,
		EdgeProjectorAbsentRetention:     edgeProjectorAbsentRetention,
		EdgeProjectorMaxAbsentIdentities: edgeProjectorMaxAbsentIdentities,
		EdgeObservationHistoryPath:       getEnv("ATHENA_EDGE_OBSERVATION_HISTORY_PATH", ""),
		EdgePostgresDSN:                  getEnv("ATHENA_EDGE_POSTGRES_DSN", ""),
		EdgeAnalyticsMaxWindow:           analyticsMaxWindow,
		FacilityCatalogPath:              getEnv("ATHENA_FACILITY_CATALOG_PATH", ""),
		DefaultFacilityID:                getEnv("ATHENA_DEFAULT_FACILITY_ID", "ashtonbee"),
		DefaultZoneID:                    getEnv("ATHENA_DEFAULT_ZONE_ID", ""),
		MockFacilityID:                   getEnv("ATHENA_MOCK_FACILITY_ID", getEnv("ATHENA_DEFAULT_FACILITY_ID", "ashtonbee")),
		MockZoneID:                       getEnv("ATHENA_MOCK_ZONE_ID", getEnv("ATHENA_DEFAULT_ZONE_ID", "")),
		MockEntries:                      entries,
		MockExits:                        exits,
		MockIdentifiedTagHashes:          splitCSV(getEnv("ATHENA_MOCK_IDENTIFIED_TAG_HASHES", "")),
		MockIdentifiedExitTagHashes:      splitCSV(getEnv("ATHENA_MOCK_IDENTIFIED_EXIT_TAG_HASHES", "")),
		CSVPath:                          getEnv("ATHENA_CSV_PATH", ""),
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
	if cfg.EdgeProjectorAbsentRetention <= 0 {
		return Config{}, fmt.Errorf("invalid ATHENA_EDGE_PROJECTOR_ABSENT_RETENTION %s: value must be > 0", cfg.EdgeProjectorAbsentRetention)
	}
	if cfg.EdgeProjectorMaxAbsentIdentities <= 0 {
		return Config{}, fmt.Errorf("invalid ATHENA_EDGE_PROJECTOR_MAX_ABSENT_IDENTITIES %d: value must be > 0", cfg.EdgeProjectorMaxAbsentIdentities)
	}
	if cfg.EdgeAnalyticsMaxWindow <= 0 {
		return Config{}, fmt.Errorf("invalid ATHENA_EDGE_ANALYTICS_MAX_WINDOW %s: value must be > 0", cfg.EdgeAnalyticsMaxWindow)
	}
	if err := validateEdgeConfig(cfg); err != nil {
		return Config{}, err
	}

	switch cfg.Adapter {
	case "mock":
		return cfg, nil
	case "csv":
		if strings.TrimSpace(cfg.CSVPath) == "" {
			return Config{}, fmt.Errorf("ATHENA_CSV_PATH is required when ATHENA_ADAPTER=csv")
		}
		return cfg, nil
	default:
		return Config{}, fmt.Errorf("invalid ATHENA_ADAPTER %q: supported values: mock, csv", cfg.Adapter)
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

func getEnvAsBool(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: %w", key, value, err)
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

func parseNodeTokenMap(value string) map[string]string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	pairs := strings.Split(value, ",")
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		nodeID, token, ok := strings.Cut(pair, "=")
		if !ok {
			return map[string]string{
				"": "",
			}
		}

		nodeID = strings.TrimSpace(nodeID)
		token = strings.TrimSpace(token)
		out[nodeID] = token
	}

	return out
}

func validateEdgeConfig(cfg Config) error {
	if cfg.EdgePostgresDSN != "" && cfg.EdgeObservationHistoryPath != "" {
		return fmt.Errorf("ATHENA_EDGE_POSTGRES_DSN and ATHENA_EDGE_OBSERVATION_HISTORY_PATH are mutually exclusive")
	}
	if cfg.EdgePolicyAcceptanceEnabled && strings.TrimSpace(cfg.EdgePostgresDSN) == "" {
		return fmt.Errorf("ATHENA_EDGE_POLICY_ACCEPTANCE_ENABLED requires ATHENA_EDGE_POSTGRES_DSN")
	}
	if cfg.EdgeOccupancyProjection && cfg.EdgeHashSalt == "" && len(cfg.EdgeTokens) == 0 {
		return fmt.Errorf("ATHENA_EDGE_OCCUPANCY_PROJECTION requires edge ingress to be enabled")
	}
	if cfg.EdgeHashSalt == "" && len(cfg.EdgeTokens) == 0 {
		return nil
	}
	if cfg.EdgeHashSalt == "" {
		return fmt.Errorf("ATHENA_EDGE_HASH_SALT is required when ATHENA_EDGE_TOKENS is set")
	}
	if len(cfg.EdgeTokens) == 0 {
		return fmt.Errorf("ATHENA_EDGE_TOKENS is required when ATHENA_EDGE_HASH_SALT is set")
	}
	if cfg.NATSURL == "" {
		return fmt.Errorf("ATHENA_NATS_URL is required when edge ingress is enabled")
	}

	for nodeID, token := range cfg.EdgeTokens {
		if nodeID == "" {
			return fmt.Errorf("invalid ATHENA_EDGE_TOKENS: entries must be node_id=token")
		}
		if token == "" {
			return fmt.Errorf("invalid ATHENA_EDGE_TOKENS: node %q is missing a token", nodeID)
		}
	}

	return nil
}
