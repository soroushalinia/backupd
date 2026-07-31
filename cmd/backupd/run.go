package main

import (
	"fmt"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/engine"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "run <plan-name>",
		Short: "Execute a backup plan immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromContext(cmd.Context())
			planName := args[0]

			var plan *config.Plan
			for i := range cfg.Plans {
				if cfg.Plans[i].Name == planName {
					plan = &cfg.Plans[i]
					break
				}
			}
			if plan == nil {
				return fmt.Errorf("plan %q not found", planName)
			}

			store, err := state.New(defaultStatePath())
			if err != nil {
				return fmt.Errorf("opening state: %w", err)
			}
			defer store.Close()

			dest, err := storage.NewFromDest(plan.Destination)
			if err != nil {
				return fmt.Errorf("storage: %w", err)
			}

			eng := engine.New(store)
			var result *engine.RunResult
			if dryRun {
				result, err = eng.DryRun(cmd.Context(), *plan, dest)
				if err == nil {
					fmt.Printf("dry run complete: %d bytes would be uploaded (%s)\n", result.Size, result.Duration)
				}
			} else {
				result, err = eng.Run(cmd.Context(), *plan, dest)
				if err == nil {
					fmt.Printf("snapshot %s complete (%d bytes in %s)\n", result.SnapshotID, result.Size, result.Duration)
				}
			}
			if err != nil {
				return fmt.Errorf("backup failed: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be uploaded without writing anything")
	return cmd
}

func defaultStatePath() string {
	return config.DefaultConfigPath() + ".db"
}
