package main

import (
	"fmt"
	"time"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/spf13/cobra"
)

func newHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history [plan-name]",
		Short: "Show all snapshots for a plan",
		Long: `Show all snapshots for a plan. With no plan name, the history of
every configured plan is shown.`,
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

				snaps, err := store.ListSnapshots(plan.Name)
				if err != nil {
					return err
				}

				if len(snaps) == 0 {
					fmt.Printf("plan %q: no snapshots\n", plan.Name)
					continue
				}

				for _, s := range snaps {
					fmt.Printf("%-36s  %s  %d bytes  %s\n",
						s.ID, s.Timestamp.Format(time.RFC3339), s.Size, plan.Name)
				}
			}
			return nil
		},
	}
}
