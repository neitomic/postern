package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/neitomic/postern/internal/agent"
	"github.com/neitomic/postern/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRoot().Execute(); err != nil {
		var be *agent.BindError
		if errors.As(err, &be) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "postern",
		Short: "Postern agent and client",
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
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.Version)
		},
	})
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newJoinCmd())
	cmd.AddCommand(newEnrollMachineCmd())
	cmd.AddCommand(newAgentCmd())
	return cmd
}
