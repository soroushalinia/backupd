package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/xero/backupd/internal/config"
	"github.com/xero/backupd/internal/scheduler"
)

func newExportSystemdCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-systemd [plan-name]",
		Short: "Generate systemd timer and service units",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromContext(cmd.Context())
			outDir, _ := cmd.Flags().GetString("output")
			binaryPath, _ := cmd.Flags().GetString("binary")
			configPath, _ := cmd.Flags().GetString("config")

			if binaryPath == "" {
				exe, err := os.Executable()
				if err != nil {
					return fmt.Errorf("finding binary path: %w", err)
				}
				binaryPath = exe
			}

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

			for _, plan := range plans {
				if plan.Schedule == "" {
					continue
				}
				units, err := scheduler.GenerateSystemd(plan.Name, plan.Schedule, binaryPath, configPath)
				if err != nil {
					return fmt.Errorf("generating units for %q: %w", plan.Name, err)
				}

				if outDir != "" {
					if err := os.WriteFile(filepath.Join(outDir, fmt.Sprintf("backupd-%s.service", plan.Name)), []byte(units.Service), 0644); err != nil {
						return fmt.Errorf("writing service unit: %w", err)
					}
					if err := os.WriteFile(filepath.Join(outDir, fmt.Sprintf("backupd-%s.timer", plan.Name)), []byte(units.Timer), 0644); err != nil {
						return fmt.Errorf("writing timer unit: %w", err)
					}
					fmt.Printf("wrote units for %q to %s\n", plan.Name, outDir)
				} else {
					fmt.Printf("=== %s.service ===\n%s\n", plan.Name, units.Service)
					fmt.Printf("=== %s.timer ===\n%s\n", plan.Name, units.Timer)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringP("output", "o", "", "output directory for unit files")
	cmd.Flags().String("binary", "", "path to backupd binary")
	return cmd
}
