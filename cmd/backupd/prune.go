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
		Use:   "prune [plan-name]",
		Short: "Apply a plan's retention policy now",
		Long: `Apply a plan's retention policy now: delete the snapshots the policy
no longer keeps, then garbage-collect orphaned blocks. Pruning normally
happens automatically after every successful backup run. With no plan
name, every configured plan is pruned.`,
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

			pruned := 0
			for i := range plans {
				plan := &plans[i]

				if plan.Retention == nil {
					if len(args) == 0 {
						fmt.Printf("plan %q has no retention policy, skipping\n", plan.Name)
						continue
					}
					return fmt.Errorf("plan %q has no retention policy", plan.Name)
				}

				dest, err := storage.NewFromDest(plan.Destination)
				if err != nil {
					return fmt.Errorf("storage: %w", err)
				}

				pruner := retention.NewPruner(store)
				pruner.DryRun = dryRun
				if err := pruner.Prune(cmd.Context(), plan.Name, retention.FromConfig(plan.Retention), dest); err != nil {
					return fmt.Errorf("plan %q prune failed: %w", plan.Name, err)
				}
				pruned++
			}

			if dryRun {
				fmt.Printf("dry run complete: no changes made to %d plan(s)\n", pruned)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be deleted without deleting anything")
	return cmd
}
