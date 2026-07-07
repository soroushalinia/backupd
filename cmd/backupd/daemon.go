package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/xero/backupd/internal/config"
	"github.com/xero/backupd/internal/scheduler"
	"github.com/xero/backupd/internal/state"
)

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the backup scheduler in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromContext(cmd.Context())

			store, err := state.New(defaultStatePath())
			if err != nil {
				return fmt.Errorf("opening state: %w", err)
			}
			defer store.Close()

			d, err := scheduler.NewDaemon(cfg, store)
			if err != nil {
				return fmt.Errorf("creating daemon: %w", err)
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			fmt.Println("backupd daemon started")
			if err := d.Run(ctx); err != nil {
				return err
			}
			fmt.Println("backupd daemon stopped")
			return nil
		},
	}
}
