package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/soroushalinia/backupd/internal/config"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured backup plans",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromContext(cmd.Context())

			if len(cfg.Plans) == 0 {
				fmt.Println("no plans configured")
				return nil
			}

			for _, p := range cfg.Plans {
				srcTypes := ""
				for _, s := range p.Sources {
					if srcTypes != "" {
						srcTypes += ", "
					}
					srcTypes += s.Type
				}
				fmt.Printf("%-20s schedule=%-15s sources=%s\n", p.Name, p.Schedule, srcTypes)
			}
			return nil
		},
	}
}
