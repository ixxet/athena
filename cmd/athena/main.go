package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

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

const (
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 15 * time.Second
	httpWriteTimeout      = 15 * time.Second
	httpIdleTimeout       = 60 * time.Second
	httpShutdownTimeout   = 10 * time.Second
)

type app struct {
	adapter     adapter.PresenceAdapter
	readPath    *presence.ReadPath
	adapterName string
}

type edgeRuntime struct {
	backend            string
	recorder           edgeingress.ObservationRecorder
	acceptanceRecorder edgeingress.AcceptedPresenceRecorder
	policyEvaluator    edgeingress.PolicyEvaluator
	markerReader       edgehistory.MarkerReader
	replayReader       edgehistory.ReplayReader
	historyReader      edgehistory.PublicObservationReader
	recentReader       edgehistory.RecentObservationReader
	analyticsReader    edgehistory.AnalyticsReader
	close              func()
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
	rootCmd.AddCommand(newPolicyCmd())
	rootCmd.AddCommand(newIdentityCmd())

	return rootCmd
}

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the ATHENA HTTP server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			serveCtx, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stopSignals()

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
				edgeStores     edgeRuntime
			)
			edgeStores, err = openEdgeRuntime(serveCtx, cfg)
			if err != nil {
				return err
			}
			defer edgeStores.close()
			if cfg.EdgeOccupancyProjection {
				publisher, closePublisher, err = newPublisherHandle(cfg)
				if err != nil {
					return err
				}
				defer closePublisher()

				projector := presence.NewProjector(
					presence.WithAbsentIdentityRetention(cfg.EdgeProjectorAbsentRetention),
					presence.WithMaxAbsentIdentities(cfg.EdgeProjectorMaxAbsentIdentities),
					presence.WithProjectionMarkerResolver(buildProjectionMarkerResolver(edgeStores.markerReader)),
				)
				if edgeStores.replayReader != nil {
					records, err := edgeStores.replayReader.ReadAll(serveCtx)
					if err != nil {
						return fmt.Errorf("load edge observation history for replay: %w", err)
					}
					replay, err := edgehistory.ReplayProjector(projector, records)
					if err != nil {
						return fmt.Errorf("rebuild edge occupancy projection from history: %w", err)
					}
					slog.Info(
						"edge observation history replayed",
						"storage_backend", edgeStores.backend,
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
				if edgeStores.recorder != nil {
					edgeOpts = append(edgeOpts, edgeingress.WithObservationRecorder(edgeStores.recorder))
				}
				if edgeStores.acceptanceRecorder != nil {
					edgeOpts = append(edgeOpts, edgeingress.WithAcceptedPresenceRecorder(edgeStores.acceptanceRecorder))
				}
				if cfg.EdgePolicyAcceptanceEnabled && edgeStores.policyEvaluator != nil {
					edgeOpts = append(edgeOpts,
						edgeingress.WithPolicyEvaluator(edgeStores.policyEvaluator),
						edgeingress.WithPolicyAcceptanceEnabled(true),
					)
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
					"storage_backend", edgeStores.backend,
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
						if err := worker.Run(serveCtx); err != nil {
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
					if edgeStores.recorder != nil {
						edgeOpts = append(edgeOpts, edgeingress.WithObservationRecorder(edgeStores.recorder))
					}
					if edgeStores.acceptanceRecorder != nil {
						edgeOpts = append(edgeOpts, edgeingress.WithAcceptedPresenceRecorder(edgeStores.acceptanceRecorder))
					}
					if cfg.EdgePolicyAcceptanceEnabled && edgeStores.policyEvaluator != nil {
						edgeOpts = append(edgeOpts,
							edgeingress.WithPolicyEvaluator(edgeStores.policyEvaluator),
							edgeingress.WithPolicyAcceptanceEnabled(true),
						)
					}
					edgeService, err := edgeingress.NewService(publisher, cfg.EdgeHashSalt, cfg.EdgeTokens, edgeOpts...)
					if err != nil {
						return err
					}
					edgeTapHandler = edgeingress.NewHandler(edgeService)
					slog.Info("edge tap ingress enabled", "node_count", len(cfg.EdgeTokens), "storage_backend", edgeStores.backend)
				}
			}

			collector := metrics.New(readPath)
			handlerOpts := make([]server.Option, 0, 4)
			if edgeTapHandler != nil {
				handlerOpts = append(handlerOpts, server.WithEdgeTapHandler(edgeTapHandler))
			}
			if edgeStores.historyReader != nil {
				handlerOpts = append(handlerOpts, server.WithHistoryReader(edgeStores.historyReader))
			}
			if edgeStores.analyticsReader != nil {
				handlerOpts = append(handlerOpts, server.WithAnalyticsReader(edgeStores.analyticsReader))
				handlerOpts = append(handlerOpts, server.WithAnalyticsMaxWindow(cfg.EdgeAnalyticsMaxWindow))
			}
			if facilityStore != nil {
				handlerOpts = append(handlerOpts, server.WithFacilityStore(facilityStore))
			}
			handler := server.NewHandler(readPath, collector, adapterName, handlerOpts...)

			httpServer := &http.Server{
				Addr:              cfg.HTTPAddr,
				Handler:           handler,
				ReadHeaderTimeout: httpReadHeaderTimeout,
				ReadTimeout:       httpReadTimeout,
				WriteTimeout:      httpWriteTimeout,
				IdleTimeout:       httpIdleTimeout,
			}

			slog.Info("starting ATHENA server", "addr", cfg.HTTPAddr, "adapter", adapterName)

			serverErrors := make(chan error, 1)
			go func() {
				serverErrors <- httpServer.ListenAndServe()
			}()

			select {
			case err := <-serverErrors:
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
			case <-serveCtx.Done():
				slog.Info("athena shutdown requested")
			}

			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), httpShutdownTimeout)
			defer cancelShutdown()

			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("shutdown ATHENA HTTP server: %w", err)
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
		historyPath        string
		historyPostgresDSN string
		historyLimit       int
		historyFormat      string
	)

	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "Inspect recent durable edge observations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if historyLimit <= 0 {
				return fmt.Errorf("--limit must be > 0")
			}
			reader, closeReader, err := openEdgeHistoryReader(cmd.Context(), historyPath, historyPostgresDSN)
			if err != nil {
				return err
			}
			defer closeReader()

			records, err := reader.ReadRecent(cmd.Context(), historyLimit)
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
						"event_id=%s facility_id=%s zone_id=%s node_id=%s direction=%s result=%s failure_reason_code=%s accepted=%t acceptance_path=%s accepted_reason_code=%s external_identity_hash=%s observed_at=%s stored_at=%s\n",
						record.EventID,
						record.FacilityID,
						record.ZoneID,
						record.NodeID,
						record.Direction,
						record.Result,
						record.FailureReasonCode,
						record.AcceptedAt != nil || (record.Result == "pass" && record.CommittedAt != nil),
						effectiveAcceptancePath(record),
						record.AcceptedReasonCode,
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
	historyCmd.Flags().StringVar(&historyPostgresDSN, "postgres-dsn", os.Getenv("ATHENA_EDGE_POSTGRES_DSN"), "dsn for the Postgres-backed edge observation store")
	historyCmd.Flags().IntVar(&historyLimit, "limit", 20, "maximum number of recent observations to print")
	historyCmd.Flags().StringVar(&historyFormat, "format", "text", "output format: text or json")

	edgeCmd.AddCommand(historyCmd)

	var (
		analyticsPostgresDSN  string
		analyticsFacilityID   string
		analyticsZoneID       string
		analyticsNodeID       string
		analyticsSince        string
		analyticsUntil        string
		analyticsBucket       int
		analyticsSessionLimit int
		analyticsFormat       string
	)

	analyticsCmd := &cobra.Command{
		Use:   "analytics",
		Short: "Read bounded edge analytics from the Postgres observation store.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := edgehistory.NewPostgresStore(cmd.Context(), analyticsPostgresDSN)
			if err != nil {
				return err
			}
			defer store.Close()

			since, err := parseRFC3339Value(analyticsSince, "--since")
			if err != nil {
				return err
			}
			until, err := parseRFC3339Value(analyticsUntil, "--until")
			if err != nil {
				return err
			}

			report, err := store.ReadAnalytics(cmd.Context(), edgehistory.AnalyticsFilter{
				FacilityID:   analyticsFacilityID,
				ZoneID:       analyticsZoneID,
				NodeID:       analyticsNodeID,
				Since:        since,
				Until:        until,
				BucketSize:   time.Duration(analyticsBucket) * time.Minute,
				SessionLimit: analyticsSessionLimit,
			})
			if err != nil {
				return err
			}

			switch analyticsFormat {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(report)
			case "text":
				if _, err := fmt.Fprintf(
					cmd.OutOrStdout(),
					"facility=%s zone=%s node=%s since=%s until=%s total=%d pass=%d fail=%d committed_pass=%d accepted=%d accepted_touchnet_pass=%d accepted_testing_policy=%d recognized_denied=%d bad_account_number=%d open_sessions=%d closed_sessions=%d unmatched_exit=%d unique_visitors=%d occupancy_at_end=%d average_duration_seconds=%d median_duration_seconds=%d\n",
					report.FacilityID,
					report.ZoneID,
					report.NodeID,
					report.Since.Format(time.RFC3339),
					report.Until.Format(time.RFC3339),
					report.ObservationSummary.Total,
					report.ObservationSummary.Pass,
					report.ObservationSummary.Fail,
					report.ObservationSummary.CommittedPass,
					report.ObservationSummary.Accepted,
					report.ObservationSummary.AcceptedTouchnetPass,
					report.ObservationSummary.AcceptedTestingPolicy,
					report.ObservationSummary.RecognizedDenied,
					report.ObservationSummary.BadAccountNumber,
					report.SessionSummary.OpenCount,
					report.SessionSummary.ClosedCount,
					report.SessionSummary.UnmatchedExitCount,
					report.SessionSummary.UniqueVisitors,
					report.SessionSummary.OccupancyAtEnd,
					report.SessionSummary.AverageDurationSeconds,
					report.SessionSummary.MedianDurationSeconds,
				); err != nil {
					return err
				}
				for _, bucket := range report.FlowBuckets {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"bucket started_at=%s ended_at=%s pass_in=%d pass_out=%d fail_in=%d fail_out=%d occupancy_end=%d\n",
						bucket.StartedAt.Format(time.RFC3339),
						bucket.EndedAt.Format(time.RFC3339),
						bucket.PassIn,
						bucket.PassOut,
						bucket.FailIn,
						bucket.FailOut,
						bucket.OccupancyEnd,
					); err != nil {
						return err
					}
				}
				for _, session := range report.Sessions {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"session session_id=%s state=%s entry_node_id=%s entry_at=%s exit_node_id=%s exit_at=%s duration_seconds=%s\n",
						session.SessionID,
						session.State,
						session.EntryNodeID,
						formatOptionalTime(session.EntryAt),
						session.ExitNodeID,
						formatOptionalTime(session.ExitAt),
						formatOptionalInt64(session.DurationSeconds),
					); err != nil {
						return err
					}
				}
				return nil
			default:
				return fmt.Errorf("unsupported format %q", analyticsFormat)
			}
		},
	}
	analyticsCmd.Flags().StringVar(&analyticsPostgresDSN, "postgres-dsn", os.Getenv("ATHENA_EDGE_POSTGRES_DSN"), "dsn for the Postgres-backed edge observation store")
	analyticsCmd.Flags().StringVar(&analyticsFacilityID, "facility", "", "facility id to query")
	analyticsCmd.Flags().StringVar(&analyticsZoneID, "zone", "", "optional zone id to query")
	analyticsCmd.Flags().StringVar(&analyticsNodeID, "node", "", "optional node id to query")
	analyticsCmd.Flags().StringVar(&analyticsSince, "since", "", "inclusive RFC3339 lower bound")
	analyticsCmd.Flags().StringVar(&analyticsUntil, "until", "", "inclusive RFC3339 upper bound")
	analyticsCmd.Flags().IntVar(&analyticsBucket, "bucket-minutes", 15, "bucket size in minutes")
	analyticsCmd.Flags().IntVar(&analyticsSessionLimit, "session-limit", 20, "maximum number of recent session facts to print")
	analyticsCmd.Flags().StringVar(&analyticsFormat, "format", "text", "output format: text or json")
	_ = analyticsCmd.MarkFlagRequired("facility")
	_ = analyticsCmd.MarkFlagRequired("since")
	_ = analyticsCmd.MarkFlagRequired("until")

	edgeCmd.AddCommand(analyticsCmd)
	return edgeCmd
}

func newPolicyCmd() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage ATHENA edge admission policies.",
	}

	var (
		postgresDSN string
		facilityID  string
		subjectID   string
		startsAt    string
		endsAt      string
		reasonCode  string
		actorKind   string
		actorID     string
		format      string
	)

	createFacilityCmd := &cobra.Command{
		Use:   "create-facility-window",
		Short: "Create a facility-wide recognized-denied testing policy window.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := edgehistory.NewPostgresStore(cmd.Context(), postgresDSN)
			if err != nil {
				return err
			}
			defer store.Close()

			start, err := parseRFC3339Value(startsAt, "--starts-at")
			if err != nil {
				return err
			}
			end, err := parseRFC3339Value(endsAt, "--ends-at")
			if err != nil {
				return err
			}
			record, err := store.CreateFacilityWindowPolicy(cmd.Context(), edgehistory.CreateFacilityWindowPolicyInput{
				FacilityID:         facilityID,
				StartsAt:           start,
				EndsAt:             end,
				ReasonCode:         reasonCode,
				CreatedByActorKind: actorKind,
				CreatedByActorID:   actorID,
				CreatedBySurface:   "athena_cli",
			})
			if err != nil {
				return err
			}
			return writePolicyRecord(cmd, record, format)
		},
	}
	createFacilityCmd.Flags().StringVar(&postgresDSN, "postgres-dsn", os.Getenv("ATHENA_EDGE_POSTGRES_DSN"), "dsn for the Postgres-backed edge store")
	createFacilityCmd.Flags().StringVar(&facilityID, "facility", "", "facility id to apply the policy to")
	createFacilityCmd.Flags().StringVar(&startsAt, "starts-at", "", "policy start time in RFC3339")
	createFacilityCmd.Flags().StringVar(&endsAt, "ends-at", "", "policy end time in RFC3339")
	createFacilityCmd.Flags().StringVar(&reasonCode, "reason-code", "testing_rollout", "reason code: testing_rollout, alumni_exception, semester_rollover, owner_exception")
	createFacilityCmd.Flags().StringVar(&actorKind, "actor-kind", "owner_user", "actor kind: owner_user, service_account, system")
	createFacilityCmd.Flags().StringVar(&actorID, "actor-id", os.Getenv("USER"), "actor id to record on the policy version")
	createFacilityCmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	_ = createFacilityCmd.MarkFlagRequired("facility")
	_ = createFacilityCmd.MarkFlagRequired("starts-at")
	_ = createFacilityCmd.MarkFlagRequired("ends-at")
	policyCmd.AddCommand(createFacilityCmd)

	var subjectMode string
	createSubjectCmd := &cobra.Command{
		Use:   "create-subject",
		Short: "Create a subject-scoped admission policy.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := edgehistory.NewPostgresStore(cmd.Context(), postgresDSN)
			if err != nil {
				return err
			}
			defer store.Close()

			start, err := parseRFC3339Value(startsAt, "--starts-at")
			if err != nil {
				return err
			}
			var endPtr *time.Time
			if strings.TrimSpace(endsAt) != "" {
				end, err := parseRFC3339Value(endsAt, "--ends-at")
				if err != nil {
					return err
				}
				endPtr = &end
			}
			record, err := store.CreateSubjectPolicy(cmd.Context(), edgehistory.CreateSubjectPolicyInput{
				FacilityID:         facilityID,
				SubjectID:          subjectID,
				PolicyMode:         subjectMode,
				StartsAt:           start,
				EndsAt:             endPtr,
				ReasonCode:         reasonCode,
				CreatedByActorKind: actorKind,
				CreatedByActorID:   actorID,
				CreatedBySurface:   "athena_cli",
			})
			if err != nil {
				return err
			}
			return writePolicyRecord(cmd, record, format)
		},
	}
	createSubjectCmd.Flags().StringVar(&postgresDSN, "postgres-dsn", os.Getenv("ATHENA_EDGE_POSTGRES_DSN"), "dsn for the Postgres-backed edge store")
	createSubjectCmd.Flags().StringVar(&facilityID, "facility", "", "facility id to apply the policy to")
	createSubjectCmd.Flags().StringVar(&subjectID, "subject-id", "", "subject id to apply the policy to")
	createSubjectCmd.Flags().StringVar(&subjectMode, "mode", "always_admit", "policy mode: always_admit or grace_until")
	createSubjectCmd.Flags().StringVar(&startsAt, "starts-at", "", "policy start time in RFC3339")
	createSubjectCmd.Flags().StringVar(&endsAt, "ends-at", "", "policy end time in RFC3339; required for grace_until")
	createSubjectCmd.Flags().StringVar(&reasonCode, "reason-code", "owner_exception", "reason code: testing_rollout, alumni_exception, semester_rollover, owner_exception")
	createSubjectCmd.Flags().StringVar(&actorKind, "actor-kind", "owner_user", "actor kind: owner_user, service_account, system")
	createSubjectCmd.Flags().StringVar(&actorID, "actor-id", os.Getenv("USER"), "actor id to record on the policy version")
	createSubjectCmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	_ = createSubjectCmd.MarkFlagRequired("facility")
	_ = createSubjectCmd.MarkFlagRequired("subject-id")
	_ = createSubjectCmd.MarkFlagRequired("starts-at")
	policyCmd.AddCommand(createSubjectCmd)

	var policyID string
	disableCmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable a policy by inserting a new disabled version.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := edgehistory.NewPostgresStore(cmd.Context(), postgresDSN)
			if err != nil {
				return err
			}
			defer store.Close()

			record, err := store.DisablePolicy(cmd.Context(), edgehistory.DisablePolicyInput{
				PolicyID:           policyID,
				CreatedByActorKind: actorKind,
				CreatedByActorID:   actorID,
				CreatedBySurface:   "athena_cli",
			})
			if err != nil {
				return err
			}
			return writePolicyRecord(cmd, record, format)
		},
	}
	disableCmd.Flags().StringVar(&postgresDSN, "postgres-dsn", os.Getenv("ATHENA_EDGE_POSTGRES_DSN"), "dsn for the Postgres-backed edge store")
	disableCmd.Flags().StringVar(&policyID, "policy-id", "", "policy id to disable")
	disableCmd.Flags().StringVar(&actorKind, "actor-kind", "owner_user", "actor kind: owner_user, service_account, system")
	disableCmd.Flags().StringVar(&actorID, "actor-id", os.Getenv("USER"), "actor id to record on the policy version")
	disableCmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	_ = disableCmd.MarkFlagRequired("policy-id")
	policyCmd.AddCommand(disableCmd)

	var activeAt string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List the latest edge admission policies for a facility.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := edgehistory.NewPostgresStore(cmd.Context(), postgresDSN)
			if err != nil {
				return err
			}
			defer store.Close()

			var activeAtPtr *time.Time
			if strings.TrimSpace(activeAt) != "" {
				parsed, err := parseRFC3339Value(activeAt, "--active-at")
				if err != nil {
					return err
				}
				activeAtPtr = &parsed
			}
			records, err := store.ListPolicies(cmd.Context(), facilityID, subjectID, activeAtPtr)
			if err != nil {
				return err
			}
			switch format {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(records)
			case "text":
				for _, record := range records {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"policy_id=%s policy_version_id=%s facility_id=%s subject_id=%s version=%d enabled=%t mode=%s target_selector=%s starts_at=%s ends_at=%s reason_code=%s actor_kind=%s actor_id=%s surface=%s created_at=%s\n",
						record.PolicyID,
						record.PolicyVersionID,
						record.FacilityID,
						record.SubjectID,
						record.VersionNumber,
						record.IsEnabled,
						record.PolicyMode,
						record.TargetSelector,
						record.StartsAt.Format(time.RFC3339),
						formatOptionalTime(record.EndsAt),
						record.ReasonCode,
						record.CreatedByActorKind,
						record.CreatedByActorID,
						record.CreatedBySurface,
						record.CreatedAt.Format(time.RFC3339),
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
	listCmd.Flags().StringVar(&postgresDSN, "postgres-dsn", os.Getenv("ATHENA_EDGE_POSTGRES_DSN"), "dsn for the Postgres-backed edge store")
	listCmd.Flags().StringVar(&facilityID, "facility", "", "facility id to list policies for")
	listCmd.Flags().StringVar(&subjectID, "subject-id", "", "optional subject id to filter by")
	listCmd.Flags().StringVar(&activeAt, "active-at", "", "optional RFC3339 time to filter to active policies only")
	listCmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	_ = listCmd.MarkFlagRequired("facility")
	policyCmd.AddCommand(listCmd)

	return policyCmd
}

func newIdentityCmd() *cobra.Command {
	identityCmd := &cobra.Command{
		Use:   "identity",
		Short: "Inspect and extend ATHENA edge identity subjects.",
	}

	subjectCmd := &cobra.Command{
		Use:   "subject",
		Short: "Inspect edge identity subjects.",
	}

	var (
		postgresDSN          string
		facilityID           string
		subjectID            string
		externalIdentityHash string
		format               string
	)
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show one facility-local edge identity subject.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := edgehistory.NewPostgresStore(cmd.Context(), postgresDSN)
			if err != nil {
				return err
			}
			defer store.Close()

			record, ok, err := store.GetIdentitySubject(cmd.Context(), facilityID, subjectID, externalIdentityHash)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("identity subject not found")
			}
			switch format {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(record)
			case "text":
				if _, err := fmt.Fprintf(
					cmd.OutOrStdout(),
					"subject_id=%s facility_id=%s created_at=%s\n",
					record.SubjectID,
					record.FacilityID,
					record.CreatedAt.Format(time.RFC3339),
				); err != nil {
					return err
				}
				for _, link := range record.Links {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"link_id=%s kind=%s key=%s source=%s account_type=%s created_at=%s\n",
						link.LinkID,
						link.LinkKind,
						link.LinkKey,
						link.LinkSource,
						link.AccountType,
						link.CreatedAt.Format(time.RFC3339),
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
	showCmd.Flags().StringVar(&postgresDSN, "postgres-dsn", os.Getenv("ATHENA_EDGE_POSTGRES_DSN"), "dsn for the Postgres-backed edge store")
	showCmd.Flags().StringVar(&facilityID, "facility", "", "facility id for the subject")
	showCmd.Flags().StringVar(&subjectID, "subject-id", "", "subject id to show")
	showCmd.Flags().StringVar(&externalIdentityHash, "external-identity-hash", "", "resolve the subject by hashed identity")
	showCmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	_ = showCmd.MarkFlagRequired("facility")
	subjectCmd.AddCommand(showCmd)

	identityCmd.AddCommand(subjectCmd)

	linkCmd := &cobra.Command{
		Use:   "link",
		Short: "Manage edge identity links.",
	}

	var (
		linkKind    string
		linkKey     string
		linkSource  string
		accountType string
	)
	addLinkCmd := &cobra.Command{
		Use:   "add",
		Short: "Attach a privacy-safe link to a facility-local subject.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := edgehistory.NewPostgresStore(cmd.Context(), postgresDSN)
			if err != nil {
				return err
			}
			defer store.Close()

			if err := store.AddIdentityLink(cmd.Context(), facilityID, subjectID, linkKind, linkKey, linkSource, accountType); err != nil {
				return err
			}
			record, ok, err := store.GetIdentitySubject(cmd.Context(), facilityID, subjectID, "")
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("identity subject not found after link add")
			}
			switch format {
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(record)
			case "text":
				_, err := fmt.Fprintf(
					cmd.OutOrStdout(),
					"subject_id=%s facility_id=%s link_kind=%s link_key=%s link_source=%s account_type=%s\n",
					record.SubjectID,
					record.FacilityID,
					linkKind,
					linkKey,
					linkSource,
					accountType,
				)
				return err
			default:
				return fmt.Errorf("unsupported format %q", format)
			}
		},
	}
	addLinkCmd.Flags().StringVar(&postgresDSN, "postgres-dsn", os.Getenv("ATHENA_EDGE_POSTGRES_DSN"), "dsn for the Postgres-backed edge store")
	addLinkCmd.Flags().StringVar(&facilityID, "facility", "", "facility id for the subject")
	addLinkCmd.Flags().StringVar(&subjectID, "subject-id", "", "subject id to extend")
	addLinkCmd.Flags().StringVar(&linkKind, "kind", "member_account", "link kind: external_identity_hash, member_account, qr_identity")
	addLinkCmd.Flags().StringVar(&linkKey, "key", "", "privacy-safe link key")
	addLinkCmd.Flags().StringVar(&linkSource, "source", "owner_confirmed", "link source: automatic_observation, self_signup, owner_confirmed, trusted_import")
	addLinkCmd.Flags().StringVar(&accountType, "account-type", "", "optional account type label")
	addLinkCmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	_ = addLinkCmd.MarkFlagRequired("facility")
	_ = addLinkCmd.MarkFlagRequired("subject-id")
	_ = addLinkCmd.MarkFlagRequired("key")
	linkCmd.AddCommand(addLinkCmd)

	identityCmd.AddCommand(linkCmd)
	return identityCmd
}

func writePolicyRecord(cmd *cobra.Command, record edgehistory.PolicyRecord, format string) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(record)
	case "text":
		_, err := fmt.Fprintf(
			cmd.OutOrStdout(),
			"policy_id=%s policy_version_id=%s facility_id=%s subject_id=%s version=%d enabled=%t mode=%s target_selector=%s starts_at=%s ends_at=%s reason_code=%s actor_kind=%s actor_id=%s surface=%s created_at=%s\n",
			record.PolicyID,
			record.PolicyVersionID,
			record.FacilityID,
			record.SubjectID,
			record.VersionNumber,
			record.IsEnabled,
			record.PolicyMode,
			record.TargetSelector,
			record.StartsAt.Format(time.RFC3339),
			formatOptionalTime(record.EndsAt),
			record.ReasonCode,
			record.CreatedByActorKind,
			record.CreatedByActorID,
			record.CreatedBySurface,
			record.CreatedAt.Format(time.RFC3339),
		)
		return err
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
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

func openEdgeRuntime(ctx context.Context, cfg config.Config) (edgeRuntime, error) {
	switch {
	case strings.TrimSpace(cfg.EdgePostgresDSN) != "":
		store, err := edgehistory.NewPostgresStore(ctx, cfg.EdgePostgresDSN)
		if err != nil {
			return edgeRuntime{}, err
		}
		return edgeRuntime{
			backend:            "postgres",
			recorder:           store,
			acceptanceRecorder: store,
			policyEvaluator:    store,
			markerReader:       store,
			replayReader:       store,
			historyReader:      store,
			recentReader:       store,
			analyticsReader:    store,
			close:              store.Close,
		}, nil
	case strings.TrimSpace(cfg.EdgeObservationHistoryPath) != "":
		store, err := edgehistory.NewFileStore(cfg.EdgeObservationHistoryPath)
		if err != nil {
			return edgeRuntime{}, err
		}
		return edgeRuntime{
			backend:       "file",
			recorder:      store,
			markerReader:  store,
			replayReader:  store,
			historyReader: store,
			recentReader:  store,
			close:         func() {},
		}, nil
	default:
		return edgeRuntime{
			backend: "disabled",
			close:   func() {},
		}, nil
	}
}

func openEdgeHistoryReader(ctx context.Context, historyPath, postgresDSN string) (edgehistory.RecentObservationReader, func(), error) {
	trimmedPath := strings.TrimSpace(historyPath)
	trimmedDSN := strings.TrimSpace(postgresDSN)
	if trimmedPath != "" && trimmedDSN != "" {
		return nil, nil, fmt.Errorf("--history-path and --postgres-dsn are mutually exclusive")
	}
	if trimmedDSN != "" {
		store, err := edgehistory.NewPostgresStore(ctx, trimmedDSN)
		if err != nil {
			return nil, nil, err
		}
		return store, store.Close, nil
	}
	if trimmedPath == "" {
		return nil, nil, fmt.Errorf("--history-path or --postgres-dsn is required")
	}
	store, err := edgehistory.NewFileStore(trimmedPath)
	if err != nil {
		return nil, nil, err
	}
	return store, func() {}, nil
}

func buildProjectionMarkerResolver(reader edgehistory.MarkerReader) presence.ProjectionMarkerResolver {
	if reader == nil {
		return nil
	}

	return func(ctx context.Context, event domain.PresenceEvent) (presence.ProjectionMarker, bool, error) {
		marker, found, err := reader.ReadMarker(ctx, edgehistory.MarkerKey{
			FacilityID:           event.FacilityID,
			ZoneID:               event.ZoneID,
			ExternalIdentityHash: event.ExternalIdentityHash,
		})
		if err != nil {
			return presence.ProjectionMarker{}, false, fmt.Errorf("read edge identity marker: %w", err)
		}
		if !found {
			return presence.ProjectionMarker{}, false, nil
		}

		return presence.ProjectionMarker{
			RecordedAt: marker.LastRecordedAt.UTC(),
			EventID:    marker.LastEventID,
		}, true, nil
	}
}

func parseRFC3339Value(value, name string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339: %w", name, err)
	}
	return parsed.UTC(), nil
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func formatOptionalInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func effectiveAcceptancePath(record edgeingress.ObservationRecord) string {
	if strings.TrimSpace(record.AcceptancePath) != "" {
		return record.AcceptancePath
	}
	if record.Result == "pass" && record.CommittedAt != nil {
		return edgeingress.AcceptancePathTouchNetPass
	}
	return ""
}
