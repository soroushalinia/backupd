package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xero/backupd/internal/config"
	"github.com/xero/backupd/internal/engine"
	"github.com/xero/backupd/internal/state"
)

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <plan-name> <snapshot-id>",
		Short: "Restore a snapshot to a local directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromContext(cmd.Context())
			planName := args[0]
			snapshotID := args[1]
			target, _ := cmd.Flags().GetString("target")

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

			dest, err := storageFromDest(plan.Destination)
			if err != nil {
				return fmt.Errorf("storage: %w", err)
			}

			eng := engine.New(store)
			if err := eng.Restore(cmd.Context(), planName, snapshotID, target, dest); err != nil {
				return fmt.Errorf("restore failed: %w", err)
			}

			fmt.Printf("restored snapshot %s to %s\n", snapshotID, target)
			return nil
		},
	}

	cmd.Flags().StringP("target", "t", ".", "restore target directory")
	return cmd
}
