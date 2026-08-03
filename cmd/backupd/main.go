package main

import (
	"os"

	"github.com/soroushalinia/backupd/internal/config"
	"github.com/spf13/cobra"
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

// newRootCmd builds the full command tree. main() only executes it, so tests
// can exercise every command through the same wiring.
func newRootCmd() *cobra.Command {
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
	cmd.AddCommand(newCheckCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newHistoryCmd())
	cmd.AddCommand(newRestoreCmd())
	cmd.AddCommand(newPruneCmd())
	cmd.AddCommand(newDaemonCmd())
	cmd.AddCommand(newExportSystemdCmd())
	cmd.AddCommand(newCompletionCmd())
	cmd.AddCommand(newVerifyCmd())

	return cmd
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
