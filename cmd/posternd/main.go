package main

import (
	"fmt"
	"os"

	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRoot().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "posternd",
		Short: "Postern VPS registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}
	cmd.Version = version.Version
	cmd.PersistentFlags().String("config", config.DefaultPosterndPath, "path to posternd.toml")
	cmd.PersistentFlags().String("socket", "", "unix socket path (overrides config and POSTERND_SOCKET)")
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.Version)
		},
	})
	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newTokenCmd())
	cmd.AddCommand(newEnrollCmd())
	cmd.AddCommand(newAuthorizedKeysCmd())
	return cmd
}
