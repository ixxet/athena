package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/ixxet/athena/internal/adapter"
	"github.com/ixxet/athena/internal/config"
	"github.com/ixxet/athena/internal/domain"
	"github.com/ixxet/athena/internal/metrics"
	"github.com/ixxet/athena/internal/presence"
	"github.com/ixxet/athena/internal/server"
)

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

			readPath, adapterName, err := buildReadPath(cfg)
			if err != nil {
				return err
			}

			collector := metrics.New(readPath)
			handler := server.NewHandler(readPath, collector, adapterName)
			httpServer := &http.Server{
				Addr:    cfg.HTTPAddr,
				Handler: handler,
			}

			slog.Info("starting ATHENA server", "addr", cfg.HTTPAddr, "adapter", cfg.Adapter)

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

	return presenceCmd
}

func buildReadPath(cfg config.Config) (*presence.ReadPath, string, error) {
	switch cfg.Adapter {
	case "mock":
		mockAdapter := adapter.NewMockAdapter(adapter.MockConfig{
			FacilityID: cfg.MockFacilityID,
			ZoneID:     cfg.MockZoneID,
			Entries:    cfg.MockEntries,
			Exits:      cfg.MockExits,
		})
		service := presence.NewService(mockAdapter)
		readPath := presence.NewReadPath(service, domain.OccupancyFilter{
			FacilityID: cfg.MockFacilityID,
			ZoneID:     cfg.MockZoneID,
		})

		return readPath, mockAdapter.Name(), nil
	default:
		return nil, "", fmt.Errorf("unsupported adapter %q", cfg.Adapter)
	}
}
