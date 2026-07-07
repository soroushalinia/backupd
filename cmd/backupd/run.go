package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/engine"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
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
			result, err := eng.Run(cmd.Context(), *plan, dest)
			if err != nil {
				return fmt.Errorf("backup failed: %w", err)
			}

			fmt.Printf("snapshot %s complete (%d bytes in %s)\n", result.SnapshotID, result.Size, result.Duration)
			return nil
		},
	}
}

func defaultStatePath() string {
	return config.DefaultConfigPath() + ".db"
}
