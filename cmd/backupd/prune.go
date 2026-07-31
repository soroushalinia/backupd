package main

import (
	"fmt"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/retention"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
	"github.com/spf13/cobra"
)

func newPruneCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "prune <plan-name>",
		Short: "Apply a plan's retention policy now",
		Long: `Apply a plan's retention policy now: delete the snapshots the policy
no longer keeps, then garbage-collect orphaned blocks. Pruning normally
happens automatically after every successful backup run.`,
		Args: cobra.ExactArgs(1),
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

			if plan.Retention == nil {
				return fmt.Errorf("plan %q has no retention policy", planName)
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

			pruner := retention.NewPruner(store)
			pruner.DryRun = dryRun
			if err := pruner.Prune(cmd.Context(), plan.Name, retention.FromConfig(plan.Retention), dest); err != nil {
				return fmt.Errorf("prune failed: %w", err)
			}

			if dryRun {
				fmt.Printf("dry run complete: no changes made to %s\n", plan.Name)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be deleted without deleting anything")
	return cmd
}
