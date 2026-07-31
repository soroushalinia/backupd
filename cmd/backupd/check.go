package main

import (
	"fmt"
	"os"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/scheduler"
	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Validate the configuration without running anything",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromContext(cmd.Context())
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("configuration invalid: %w", err)
			}

			problems := 0
			for _, plan := range cfg.Plans {
				fmt.Printf("plan %q:\n", plan.Name)
				for _, src := range plan.Sources {
					switch src.Type {
					case "file":
						fmt.Printf("  file source: %s\n", src.Path)
						if len(src.Exclude) > 0 {
							fmt.Printf("    exclude: %v\n", src.Exclude)
						}
						if _, err := os.Stat(src.Path); err != nil {
							fmt.Printf("  warning: file source path %q not accessible: %v\n", src.Path, err)
							problems++
						}
					case "database":
						fmt.Printf("  database source: adapter=%s dsn=%s\n", src.Adapter, src.DSN)
					case "docker":
						fmt.Printf("  docker source: volume=%s\n", src.Volume)
					case "kubernetes":
						fmt.Printf("  kubernetes source: pvc=%s\n", src.PVC)
					}
				}

				fmt.Printf("  destination: %s bucket=%s endpoint=%s\n",
					plan.Destination.Type, plan.Destination.Bucket, plan.Destination.Endpoint)

				if plan.Schedule == "" {
					fmt.Printf("  schedule: none (manual runs only)\n")
				} else if err := scheduler.ValidateSchedule(plan.Schedule); err != nil {
					fmt.Printf("  warning: schedule %q invalid: %v\n", plan.Schedule, err)
					problems++
				} else {
					fmt.Printf("  schedule: %s\n", plan.Schedule)
				}

				if plan.Encryption != nil {
					if plan.Encryption.Passphrase == "" {
						fmt.Printf("  warning: encryption enabled but passphrase is empty\n")
						problems++
					} else {
						fmt.Printf("  encryption: AES-256-GCM (Argon2id key derivation)\n")
					}
				} else {
					fmt.Printf("  encryption: disabled\n")
				}

				if plan.Retention != nil {
					fmt.Printf("  retention: last=%d daily=%d weekly=%d monthly=%d\n",
						plan.Retention.KeepLast, plan.Retention.KeepDaily,
						plan.Retention.KeepWeekly, plan.Retention.KeepMonthly)
				} else {
					fmt.Printf("  retention: disabled (snapshots kept forever)\n")
				}
			}

			if problems > 0 {
				return fmt.Errorf("%d warning(s) found", problems)
			}
			fmt.Printf("configuration OK\n")
			return nil
		},
	}
}
