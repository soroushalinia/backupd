package main

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/xero/backupd/internal/config"
)

var version = "dev"

func needsConfig(cmd *cobra.Command) bool {
	for _, c := range []string{"completion", "help", "backupd"} {
		if cmd.Name() == c {
			return false
		}
	}
	return true
}

func main() {
	cmd := &cobra.Command{
		Use:          "backupd",
		Short:        "Declarative S3-compatible backup daemon",
		SilenceUsage: true,
		Version:      version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if !needsConfig(cmd) {
				return nil
			}
			cfgPath, _ := cmd.Flags().GetString("config")
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			cmd.SetContext(config.WithConfig(cmd.Context(), cfg))
			return nil
		},
	}

	cmd.PersistentFlags().StringP("config", "c", config.DefaultConfigPath(), "path to config file")

	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newHistoryCmd())
	cmd.AddCommand(newRestoreCmd())
	cmd.AddCommand(newDaemonCmd())
	cmd.AddCommand(newExportSystemdCmd())
	cmd.AddCommand(newCompletionCmd())
	cmd.AddCommand(newVerifyCmd())

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
