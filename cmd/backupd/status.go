package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/state"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [plan-name]",
		Short: "Show last backup status for one or all plans",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromContext(cmd.Context())

			store, err := state.New(defaultStatePath())
			if err != nil {
				return fmt.Errorf("opening state: %w", err)
			}
			defer store.Close()

			plans := cfg.Plans
			if len(args) == 1 {
				found := false
				for _, p := range plans {
					if p.Name == args[0] {
						plans = []config.Plan{p}
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("plan %q not found", args[0])
				}
			}

			for _, p := range plans {
				last, _ := store.LastSnapshot(p.Name)
				if last != nil {
					fmt.Printf("%-20s last=%s size=%d\n",
						p.Name, last.Timestamp.Format(time.RFC3339), last.Size)
				} else {
					fmt.Printf("%-20s last=never\n", p.Name)
				}
			}
			return nil
		},
	}
}
