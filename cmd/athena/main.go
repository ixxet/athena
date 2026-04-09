package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"

	protoevents "github.com/ixxet/ashton-proto/events"
	"github.com/ixxet/athena/internal/adapter"
	"github.com/ixxet/athena/internal/config"
	"github.com/ixxet/athena/internal/domain"
	edgeingress "github.com/ixxet/athena/internal/edge"
	"github.com/ixxet/athena/internal/edgehistory"
	"github.com/ixxet/athena/internal/facility"
	"github.com/ixxet/athena/internal/metrics"
	"github.com/ixxet/athena/internal/presence"
	"github.com/ixxet/athena/internal/publish"
	"github.com/ixxet/athena/internal/server"
	"github.com/ixxet/athena/internal/touchnet"
)

type app struct {
	adapter     adapter.PresenceAdapter
	readPath    *presence.ReadPath
	adapterName string
}

var newPublisherHandle = func(cfg config.Config) (publish.Publisher, func() error, error) {
	if cfg.NATSURL == "" {
		return nil, nil, fmt.Errorf("ATHENA_NATS_URL is required")
	}

	conn, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return nil, nil, err
	}

	return publish.NewNATSPublisher(conn), func() error {
		conn.Close()
		return nil
	}, nil
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "athena",
		Short:         "ATHENA tracks facility presence and occupancy.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(newServeCmd())
	rootCmd.AddCommand(newPresenceCmd())
	rootCmd.AddCommand(newFacilityCmd())
	rootCmd.AddCommand(newEdgeCmd())

	return rootCmd
}

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the ATHENA HTTP server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			facilityStore, err := buildFacilityStore(cfg)
			if err != nil {
				return err
			}

			var (
				readPath       *presence.ReadPath
				adapterName    string
				publisher      publish.Publisher
				closePublisher func() error
				edgeTapHandler http.Handler
				historyStore   *edgehistory.FileStore
			)
			if cfg.EdgeObservationHistoryPath != "" {
				historyStore, err = edgehistory.NewFileStore(cfg.EdgeObservationHistoryPath)
				if err != nil {
					return err
				}
			}
			if cfg.EdgeOccupancyProjection {
				publisher, closePublisher, err = newPublisherHandle(cfg)
				if err != nil {
					return err
				}
				defer closePublisher()

				projector := presence.NewProjector()
				if historyStore != nil {
					replay, err := edgehistory.ReplayFile(historyStore.Path(), projector)
					if err != nil {
						return fmt.Errorf("rebuild edge occupancy projection from history: %w", err)
					}
					slog.Info(
						"edge observation history replayed",
						"path", historyStore.Path(),
						"observations_total", replay.Total,
						"pass_total", replay.Pass,
						"fail_total", replay.Fail,
						"applied_total", replay.Applied,
						"observed_total", replay.Observed,
					)
				}
				readPath = presence.NewReadPath(projector, defaultOccupancyFilter(cfg))
				adapterName = "edge-projection"

				edgeOpts := []edgeingress.Option{
					edgeingress.WithProjection(projector),
				}
				if historyStore != nil {
					edgeOpts = append(edgeOpts, edgeingress.WithObservationRecorder(historyStore))
				}
				edgeService, err := edgeingress.NewService(
					publisher,
					cfg.EdgeHashSalt,
					cfg.EdgeTokens,
					edgeOpts...,
				)
				if err != nil {
					return err
				}
				edgeTapHandler = edgeingress.NewHandler(edgeService)
				slog.Info(
					"edge occupancy projection enabled",
					"occupancy_source", "edge-projection",
					"history_path", cfg.EdgeObservationHistoryPath,
					"node_count", len(cfg.EdgeTokens),
				)
			} else {
				application, err := buildApp(cfg)
				if err != nil {
					return err
				}

				readPath = application.readPath
				adapterName = application.adapterName

				if cfg.NATSURL != "" {
					publisher, closePublisher, err = newPublisherHandle(cfg)
					if err != nil {
						return err
					}
					defer closePublisher()

					worker := publish.NewWorker(
						publish.NewService(application.adapter, publisher),
						cfg.IdentifiedPublishInterval,
					)
					go func() {
						if err := worker.Run(cmd.Context()); err != nil {
							slog.Error("identified presence publisher stopped", "error", err)
						}
					}()
					slog.Info(
						"identified presence publisher enabled",
						"subjects", []string{
							protoevents.SubjectIdentifiedPresenceArrived,
							protoevents.SubjectIdentifiedPresenceDeparted,
						},
						"interval", cfg.IdentifiedPublishInterval.String(),
					)
				}

				if cfg.EdgeHashSalt != "" || len(cfg.EdgeTokens) > 0 {
					edgeOpts := make([]edgeingress.Option, 0, 1)
					if historyStore != nil {
						edgeOpts = append(edgeOpts, edgeingress.WithObservationRecorder(historyStore))
					}
					edgeService, err := edgeingress.NewService(publisher, cfg.EdgeHashSalt, cfg.EdgeTokens, edgeOpts...)
					if err != nil {
						return err
					}
					edgeTapHandler = edgeingress.NewHandler(edgeService)
					slog.Info("edge tap ingress enabled", "node_count", len(cfg.EdgeTokens), "history_path", cfg.EdgeObservationHistoryPath)
				}
			}

			collector := metrics.New(readPath)
			handlerOpts := make([]server.Option, 0, 1)
			if edgeTapHandler != nil {
				handlerOpts = append(handlerOpts, server.WithEdgeTapHandler(edgeTapHandler))
			}
			handlerOpts = append(handlerOpts, server.WithHistoryPath(cfg.EdgeObservationHistoryPath))
			if facilityStore != nil {
				handlerOpts = append(handlerOpts, server.WithFacilityStore(facilityStore))
			}
			handler := server.NewHandler(readPath, collector, adapterName, handlerOpts...)

			httpServer := &http.Server{
				Addr:    cfg.HTTPAddr,
				Handler: handler,
			}

			slog.Info("starting ATHENA server", "addr", cfg.HTTPAddr, "adapter", adapterName)

			if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}

			return nil
		},
	}
}

func newPresenceCmd() *cobra.Command {
	presenceCmd := &cobra.Command{
		Use:   "presence",
		Short: "Query presence and occupancy views.",
	}

	var facilityID string
	var zoneID string
	var format string

	countCmd := &cobra.Command{
		Use:   "count",
		Short: "Show the current occupancy count.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			readPath, _, err := buildReadPath(cfg)
			if err != nil {
				return err
			}

			filter := domain.OccupancyFilter{
				FacilityID: facilityID,
				ZoneID:     zoneID,
			}

			snapshot, err := readPath.CurrentOccupancy(cmd.Context(), filter)
			if err != nil {
				return err
			}

			switch format {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(snapshot)
			case "text":
				_, err := fmt.Fprintf(
					cmd.OutOrStdout(),
					"facility=%s zone=%s current_count=%d observed_at=%s\n",
					snapshot.FacilityID,
					snapshot.ZoneID,
					snapshot.CurrentCount,
					snapshot.ObservedAt.Format("2006-01-02T15:04:05Z07:00"),
				)
				return err
			default:
				return fmt.Errorf("unsupported format %q", format)
			}
		},
	}

	countCmd.Flags().StringVar(&facilityID, "facility", "", "filter by facility id")
	countCmd.Flags().StringVar(&zoneID, "zone", "", "filter by zone id")
	countCmd.Flags().StringVar(&format, "format", "text", "output format: text or json")

	presenceCmd.AddCommand(countCmd)

	var publishFormat string
	publishCmd := &cobra.Command{
		Use:   "publish-identified",
		Short: "Publish identified arrival events.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			application, err := buildApp(cfg)
			if err != nil {
				return err
			}

			publisher, closePublisher, err := newPublisherHandle(cfg)
			if err != nil {
				return err
			}
			defer closePublisher()

			published, err := publish.NewService(application.adapter, publisher).PublishArrivals(cmd.Context())
			if err != nil {
				return err
			}

			switch publishFormat {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(map[string]any{
					"subject":         protoevents.SubjectIdentifiedPresenceArrived,
					"published_count": published,
				})
			case "text":
				_, err := fmt.Fprintf(
					cmd.OutOrStdout(),
					"subject=%s published_count=%d\n",
					protoevents.SubjectIdentifiedPresenceArrived,
					published,
				)
				return err
			default:
				return fmt.Errorf("unsupported format %q", publishFormat)
			}
		},
	}
	publishCmd.Flags().StringVar(&publishFormat, "format", "text", "output format: text or json")
	presenceCmd.AddCommand(publishCmd)

	departurePublishCmd := &cobra.Command{
		Use:   "publish-identified-departures",
		Short: "Publish identified departure events.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			application, err := buildApp(cfg)
			if err != nil {
				return err
			}

			publisher, closePublisher, err := newPublisherHandle(cfg)
			if err != nil {
				return err
			}
			defer closePublisher()

			published, err := publish.NewService(application.adapter, publisher).PublishDepartures(cmd.Context())
			if err != nil {
				return err
			}

			switch publishFormat {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(map[string]any{
					"subject":         protoevents.SubjectIdentifiedPresenceDeparted,
					"published_count": published,
				})
			case "text":
				_, err := fmt.Fprintf(
					cmd.OutOrStdout(),
					"subject=%s published_count=%d\n",
					protoevents.SubjectIdentifiedPresenceDeparted,
					published,
				)
				return err
			default:
				return fmt.Errorf("unsupported format %q", publishFormat)
			}
		},
	}
	departurePublishCmd.Flags().StringVar(&publishFormat, "format", "text", "output format: text or json")
	presenceCmd.AddCommand(departurePublishCmd)

	return presenceCmd
}

func newFacilityCmd() *cobra.Command {
	facilityCmd := &cobra.Command{
		Use:   "facility",
		Short: "Query ATHENA facility truth.",
	}

	var (
		catalogPath string
		format      string
	)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List facility catalog summaries.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := loadFacilityStore(catalogPath)
			if err != nil {
				return err
			}

			summaries := store.List()
			switch format {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(map[string]any{
					"facilities": summaries,
				})
			case "text":
				for _, summary := range summaries {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"facility_id=%s name=%s timezone=%s\n",
						summary.FacilityID,
						summary.Name,
						summary.Timezone,
					); err != nil {
						return err
					}
				}
				return nil
			default:
				return fmt.Errorf("unsupported format %q", format)
			}
		},
	}
	listCmd.Flags().StringVar(&catalogPath, "catalog-path", os.Getenv("ATHENA_FACILITY_CATALOG_PATH"), "path to the ATHENA facility catalog file")
	listCmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	facilityCmd.AddCommand(listCmd)

	var facilityID string
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show one facility detail.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := loadFacilityStore(catalogPath)
			if err != nil {
				return err
			}

			record, ok := store.Facility(facilityID)
			if !ok {
				return fmt.Errorf("facility %q not found", strings.TrimSpace(facilityID))
			}

			switch format {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(record)
			case "text":
				if _, err := fmt.Fprintf(
					cmd.OutOrStdout(),
					"facility_id=%s name=%s timezone=%s\n",
					record.FacilityID,
					record.Name,
					record.Timezone,
				); err != nil {
					return err
				}
				for _, hours := range record.Hours {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"hour day=%s opens_at=%s closes_at=%s\n",
						hours.Day,
						hours.OpensAt,
						hours.ClosesAt,
					); err != nil {
						return err
					}
				}
				for _, zone := range record.Zones {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"zone zone_id=%s name=%s\n",
						zone.ZoneID,
						zone.Name,
					); err != nil {
						return err
					}
				}
				for _, closure := range record.ClosureWindows {
					zoneIDs := strings.Join(closure.ZoneIDs, ",")
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"closure starts_at=%s ends_at=%s code=%s reason=%s zone_ids=%s\n",
						closure.StartsAt,
						closure.EndsAt,
						closure.Code,
						closure.Reason,
						zoneIDs,
					); err != nil {
						return err
					}
				}
				for _, key := range sortedKeys(record.Metadata) {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"metadata key=%s value=%s\n",
						key,
						record.Metadata[key],
					); err != nil {
						return err
					}
				}
				return nil
			default:
				return fmt.Errorf("unsupported format %q", format)
			}
		},
	}
	showCmd.Flags().StringVar(&catalogPath, "catalog-path", os.Getenv("ATHENA_FACILITY_CATALOG_PATH"), "path to the ATHENA facility catalog file")
	showCmd.Flags().StringVar(&facilityID, "facility", "", "facility id to show")
	showCmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	_ = showCmd.MarkFlagRequired("facility")
	facilityCmd.AddCommand(showCmd)

	return facilityCmd
}

func newEdgeCmd() *cobra.Command {
	edgeCmd := &cobra.Command{
		Use:   "edge",
		Short: "Work with ATHENA edge ingress tools.",
	}

	var (
		csvPath       string
		facilityID    string
		zoneID        string
		entryLocation string
		exitLocation  string
		baseURL       string
		nodeID        string
		token         string
		timeScale     float64
		replayFormat  string
	)

	replayCmd := &cobra.Command{
		Use:   "replay-touchnet",
		Short: "Replay a raw TouchNet access report into ATHENA edge ingress.",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, err := os.Open(csvPath)
			if err != nil {
				return err
			}
			defer file.Close()

			records, err := touchnet.ParseAccessReport(file)
			if err != nil {
				return err
			}

			replayer, err := touchnet.NewReplayer(touchnet.ReplayConfig{
				FacilityID:    facilityID,
				ZoneID:        zoneID,
				EntryLocation: entryLocation,
				ExitLocation:  exitLocation,
				BaseURL:       baseURL,
				NodeID:        nodeID,
				Token:         token,
				TimeScale:     timeScale,
			})
			if err != nil {
				return err
			}

			sent, err := replayer.Replay(cmd.Context(), records)
			if err != nil {
				return err
			}

			switch replayFormat {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(map[string]any{
					"replayed_count": sent,
					"csv_path":       csvPath,
					"facility_id":    facilityID,
					"node_id":        nodeID,
					"time_scale":     timeScale,
				})
			case "text":
				_, err := fmt.Fprintf(
					cmd.OutOrStdout(),
					"replayed_count=%d csv_path=%s facility_id=%s node_id=%s time_scale=%g\n",
					sent,
					csvPath,
					facilityID,
					nodeID,
					timeScale,
				)
				return err
			default:
				return fmt.Errorf("unsupported format %q", replayFormat)
			}
		},
	}

	replayCmd.Flags().StringVar(&csvPath, "csv-path", "", "path to the raw TouchNet CSV export")
	replayCmd.Flags().StringVar(&facilityID, "facility", "ashtonbee", "facility id to publish")
	replayCmd.Flags().StringVar(&zoneID, "zone", "", "optional zone id to publish")
	replayCmd.Flags().StringVar(&entryLocation, "entry-location", "", "exact LOCATION value that maps to an arrival")
	replayCmd.Flags().StringVar(&exitLocation, "exit-location", "", "exact LOCATION value that maps to a departure")
	replayCmd.Flags().StringVar(&baseURL, "base-url", "http://127.0.0.1:8080", "base URL for the ATHENA HTTP server")
	replayCmd.Flags().StringVar(&nodeID, "node-id", "", "node id to authenticate with edge ingress")
	replayCmd.Flags().StringVar(&token, "token", "", "edge token for the supplied node id")
	replayCmd.Flags().Float64Var(&timeScale, "time-scale", 1.0, "replay timing scale; 1.0 preserves source deltas, 0 disables sleeps")
	replayCmd.Flags().StringVar(&replayFormat, "format", "text", "output format: text or json")
	_ = replayCmd.MarkFlagRequired("csv-path")
	_ = replayCmd.MarkFlagRequired("entry-location")
	_ = replayCmd.MarkFlagRequired("exit-location")
	_ = replayCmd.MarkFlagRequired("node-id")
	_ = replayCmd.MarkFlagRequired("token")

	edgeCmd.AddCommand(replayCmd)

	var (
		historyPath   string
		historyLimit  int
		historyFormat string
	)

	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "Inspect recent durable edge observations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if historyLimit <= 0 {
				return fmt.Errorf("--limit must be > 0")
			}
			trimmedPath := strings.TrimSpace(historyPath)
			if trimmedPath == "" {
				return fmt.Errorf("--history-path is required")
			}

			records, err := edgehistory.ReadRecent(trimmedPath, historyLimit)
			if err != nil {
				return err
			}

			switch historyFormat {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(records)
			case "text":
				for _, record := range records {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"event_id=%s facility_id=%s zone_id=%s node_id=%s direction=%s result=%s external_identity_hash=%s observed_at=%s stored_at=%s\n",
						record.EventID,
						record.FacilityID,
						record.ZoneID,
						record.NodeID,
						record.Direction,
						record.Result,
						record.ExternalIdentityHash,
						record.ObservedAt.Format("2006-01-02T15:04:05Z07:00"),
						record.StoredAt.Format("2006-01-02T15:04:05Z07:00"),
					); err != nil {
						return err
					}
				}
				return nil
			default:
				return fmt.Errorf("unsupported format %q", historyFormat)
			}
		},
	}
	historyCmd.Flags().StringVar(&historyPath, "history-path", os.Getenv("ATHENA_EDGE_OBSERVATION_HISTORY_PATH"), "path to the durable edge observation history file")
	historyCmd.Flags().IntVar(&historyLimit, "limit", 20, "maximum number of recent observations to print")
	historyCmd.Flags().StringVar(&historyFormat, "format", "text", "output format: text or json")

	edgeCmd.AddCommand(historyCmd)
	return edgeCmd
}

func buildReadPath(cfg config.Config) (*presence.ReadPath, string, error) {
	application, err := buildApp(cfg)
	if err != nil {
		return nil, "", err
	}

	return application.readPath, application.adapterName, nil
}

func buildFacilityStore(cfg config.Config) (*facility.Store, error) {
	if strings.TrimSpace(cfg.FacilityCatalogPath) == "" {
		return nil, nil
	}

	return facility.Load(cfg.FacilityCatalogPath)
}

func loadFacilityStore(path string) (*facility.Store, error) {
	store, err := facility.Load(path)
	if err != nil {
		if errors.Is(err, facility.ErrCatalogNotConfigured) {
			return nil, fmt.Errorf("ATHENA_FACILITY_CATALOG_PATH or --catalog-path is required")
		}
		return nil, err
	}

	return store, nil
}

func buildApp(cfg config.Config) (*app, error) {
	defaultFilter := defaultOccupancyFilter(cfg)

	switch cfg.Adapter {
	case "mock":
		mockAdapter := adapter.NewMockAdapter(adapter.MockConfig{
			FacilityID:              cfg.MockFacilityID,
			ZoneID:                  cfg.MockZoneID,
			Entries:                 cfg.MockEntries,
			Exits:                   cfg.MockExits,
			IdentifiedTagHashes:     cfg.MockIdentifiedTagHashes,
			IdentifiedExitTagHashes: cfg.MockIdentifiedExitTagHashes,
		})
		service := presence.NewService(mockAdapter)
		readPath := presence.NewReadPath(service, defaultFilter)

		return &app{
			adapter:     mockAdapter,
			readPath:    readPath,
			adapterName: mockAdapter.Name(),
		}, nil
	case "csv":
		csvAdapter, err := adapter.NewCSVAdapter(adapter.CSVConfig{
			Path: cfg.CSVPath,
		}, slog.Default())
		if err != nil {
			return nil, err
		}

		service := presence.NewService(csvAdapter)
		readPath := presence.NewReadPath(service, defaultFilter)

		return &app{
			adapter:     csvAdapter,
			readPath:    readPath,
			adapterName: csvAdapter.Name(),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported adapter %q", cfg.Adapter)
	}
}

func defaultOccupancyFilter(cfg config.Config) domain.OccupancyFilter {
	return domain.OccupancyFilter{
		FacilityID: cfg.DefaultFacilityID,
		ZoneID:     cfg.DefaultZoneID,
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
