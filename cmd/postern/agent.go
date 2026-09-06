package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/neitomic/postern/internal/agent"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Install and run the local tunnel agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
	}
	cmd.AddCommand(newAgentInstallCmd())
	cmd.AddCommand(newAgentUninstallCmd())
	cmd.AddCommand(newAgentEnableCmd())
	cmd.AddCommand(newAgentDisableCmd())
	cmd.AddCommand(newAgentStatusCmd())
	cmd.AddCommand(newAgentRunCmd())
	cmd.AddCommand(newAgentSetNameCmd())
	return cmd
}

func newAgentInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Write LaunchAgent plist or systemd --user unit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, p, err := loadConfigured()
			if err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			return agent.Install(p, exe, home)
		},
		SilenceUsage: true,
	}
}

func newAgentUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Disable the agent and remove the unit file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			return agent.Uninstall(home)
		},
		SilenceUsage: true,
	}
}

func newAgentEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Load and start the agent unit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			return agent.Enable(home, cmd.ErrOrStderr())
		},
		SilenceUsage: true,
	}
}

func newAgentDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Stop the agent unit (leave the unit file)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			return agent.Disable(home, cmd.ErrOrStderr())
		},
		SilenceUsage: true,
	}
}

func newAgentStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the agent unit is loaded",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			return agent.Status(home, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
		SilenceUsage: true,
	}
}

func newAgentRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run autossh and heartbeat in the foreground",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, p, err := loadConfigured()
			if err != nil {
				return err
			}
			return agent.Run(cmd.Context(), p, agent.RunOptions{})
		},
		SilenceUsage: true,
	}
}

func newAgentSetNameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-name NAME",
		Short: "Rewrite local name after hosts rename",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, p, err := loadConfigured()
			if err != nil {
				return err
			}
			name := strings.ToLower(strings.TrimSpace(args[0]))
			if err := agent.SetName(p, name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "name set to %s\n", name)
			return nil
		},
		SilenceUsage: true,
	}
}
