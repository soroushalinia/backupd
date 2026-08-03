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
		Use:   "run [plan-name]",
		Short: "Execute a backup plan immediately",
		Long: `Execute a backup plan immediately. With no plan name, every
configured plan is run in order.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromContext(cmd.Context())

			plans, err := selectPlans(cfg, args)
			if err != nil {
				return err
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
					return fmt.Errorf("plan %q storage: %w", plan.Name, err)
				}

				eng := engine.New(store)
				var result *engine.RunResult
				if dryRun {
					result, err = eng.DryRun(cmd.Context(), *plan, dest)
					if err == nil {
						fmt.Printf("plan %q dry run complete: %d bytes would be uploaded (%s)\n", plan.Name, result.Size, result.Duration)
					}
				} else {
					result, err = eng.Run(cmd.Context(), *plan, dest)
					if err == nil && result.SnapshotID == "" {
						fmt.Printf("plan %q: nothing changed, no snapshot created\n", plan.Name)
					} else if err == nil {
						fmt.Printf("plan %q snapshot %s complete (%d bytes in %s)\n", plan.Name, result.SnapshotID, result.Size, result.Duration)
					}
				}
				if err != nil {
					return fmt.Errorf("plan %q backup failed: %w", plan.Name, err)
				}
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
