package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xero/backupd/internal/config"
	"github.com/xero/backupd/internal/engine"
	"github.com/xero/backupd/internal/state"
	"github.com/xero/backupd/internal/storage"
)

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <plan-name> [snapshot-id]",
		Short: "Verify integrity of stored snapshots",
		Args:  cobra.RangeArgs(1, 2),
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
			var snapshotID string
			if len(args) == 2 {
				snapshotID = args[1]
			}

			if err := eng.Verify(cmd.Context(), planName, snapshotID, dest); err != nil {
				return fmt.Errorf("verification failed: %w", err)
			}
			fmt.Println("verification passed")
			return nil
		},
	}
}
