package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/xero/backupd/internal/state"
)

func newHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <plan-name>",
		Short: "Show all snapshots for a plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := state.New(defaultStatePath())
			if err != nil {
				return fmt.Errorf("opening state: %w", err)
			}
			defer store.Close()

			snaps, err := store.ListSnapshots(args[0])
			if err != nil {
				return err
			}

			if len(snaps) == 0 {
				fmt.Printf("no snapshots for plan %q\n", args[0])
				return nil
			}

			for _, s := range snaps {
				fmt.Printf("%-36s  %s  %d bytes\n",
					s.ID, s.Timestamp.Format(time.RFC3339), s.Size)
			}
			return nil
		},
	}
}
