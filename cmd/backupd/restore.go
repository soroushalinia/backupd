package main

import (
	"fmt"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/engine"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	var dryRun bool

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

			dest, err := storage.NewFromDest(plan.Destination)
			if err != nil {
				return fmt.Errorf("storage: %w", err)
			}

			eng := engine.New(store)
			if dryRun {
				reports, err := eng.RestoreDryRun(cmd.Context(), *plan, snapshotID, dest)
				if err != nil {
					return fmt.Errorf("dry run failed: %w", err)
				}
				for _, rep := range reports {
					switch rep.Type {
					case "file":
						fmt.Printf("would restore %d file(s), %d bytes in %d blocks\n", rep.Files, rep.Size, rep.Blocks)
					case "database":
						fmt.Printf("would restore database dump (%d blocks)\n", rep.Blocks)
					default:
						status := "missing"
						if rep.Available {
							status = "present"
						}
						fmt.Printf("would restore archive %s (%s)\n", rep.Key, status)
					}
				}
				return nil
			}

			if err := eng.Restore(cmd.Context(), *plan, snapshotID, target, dest); err != nil {
				return fmt.Errorf("restore failed: %w", err)
			}

			fmt.Printf("restored snapshot %s to %s\n", snapshotID, target)
			return nil
		},
	}

	cmd.Flags().StringP("target", "t", ".", "restore target directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be restored without writing anything")
	return cmd
}
