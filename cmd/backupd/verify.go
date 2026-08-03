package main

import (
	"fmt"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/engine"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
	"github.com/spf13/cobra"
)

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify [plan-name] [snapshot-id]",
		Short: "Verify integrity of stored snapshots",
		Long: `Verify integrity of stored snapshots by downloading and checking
every referenced block against its recorded content hash. With no plan
name, every configured plan is verified. A snapshot-id only makes sense
together with a plan name.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromContext(cmd.Context())

			plans, err := selectPlans(cfg, args)
			if err != nil {
				return err
			}

			var snapshotID string
			if len(args) == 2 {
				snapshotID = args[1]
			}

			store, err := state.New(defaultStatePath())
			if err != nil {
				return fmt.Errorf("opening state: %w", err)
			}
			defer store.Close()

			for i := range plans {
				plan := &plans[i]

				dest, err := storage.NewFromDest(plan.Destination)
				if err != nil {
					return fmt.Errorf("storage: %w", err)
				}

				eng := engine.New(store)
				if err := eng.Verify(cmd.Context(), *plan, snapshotID, dest); err != nil {
					return fmt.Errorf("plan %q verification failed: %w", plan.Name, err)
				}
				fmt.Printf("plan %q verification passed\n", plan.Name)
			}
			return nil
		},
	}
}
