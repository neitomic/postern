package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/neitomic/postern/internal/agent"
	"github.com/neitomic/postern/internal/config"
	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	var (
		binDir    string
		noEnable  bool
		noService bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Copy postern to ~/.local/bin and enable the agent service",
		Long: `Install this binary to ~/.local/bin/postern, write a LaunchAgent (macOS)
or systemd --user unit (Linux), and enable it so the agent starts on login.

The unit points at the installed path, not a Downloads/ folder. The agent
waits until this machine is enrolled (postern config + join). Install autossh
before the tunnel can come up.

postern agent install still only writes the unit and requires enrollment.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd, binDir, noEnable, noService)
		},
		SilenceUsage: true,
	}
	cmd.Flags().StringVar(&binDir, "bin-dir", "", "directory for the postern binary (default ~/.local/bin)")
	cmd.Flags().BoolVar(&noEnable, "no-enable", false, "write the unit but do not enable/start it")
	cmd.Flags().BoolVar(&noService, "no-service", false, "copy the binary only; skip LaunchAgent/systemd")
	return cmd
}

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Disable the agent and remove the unit (keeps ~/.local/bin/postern)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			if err := agent.Uninstall(home); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "agent service removed")
			return nil
		},
		SilenceUsage: true,
	}
}

func runInstall(cmd *cobra.Command, binDir string, noEnable, noService bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if binDir == "" {
		binDir = agent.DefaultBinDir(home)
	}
	src, err := os.Executable()
	if err != nil {
		return err
	}
	dst := filepath.Join(binDir, "postern")
	if err := agent.InstallBinary(src, dst); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}
	p, err := agent.DefaultPaths()
	if err != nil {
		return err
	}
	if err := p.Mkdir(); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", dst)
	if hint := agent.PATHHint(binDir); hint != "" {
		fmt.Fprint(cmd.ErrOrStderr(), hint)
	}
	if hint := agent.AutosshHint(); hint != "" {
		fmt.Fprint(cmd.ErrOrStderr(), hint)
	}

	enrolled := isEnrolled(p)
	if enrolled {
		if cfg, err := config.LoadClient(p.ConfigFile()); err == nil {
			if st, err := agent.LoadState(p.StateFile()); err == nil && st.Port > 0 {
				_ = agent.WriteSSHConfigs(p, st, cfg)
			}
		}
	}

	if noService {
		fmt.Fprint(cmd.OutOrStdout(), agent.NextSteps(enrolled))
		return nil
	}

	if err := agent.WriteAgentUnit(p, dst, home); err != nil {
		return err
	}
	if !noEnable {
		if err := agent.Enable(home, cmd.ErrOrStderr()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "agent service enabled (autostart on login)")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "agent unit written; run postern agent enable to start")
	}
	fmt.Fprint(cmd.OutOrStdout(), agent.NextSteps(enrolled))
	return nil
}

func isEnrolled(p agent.Paths) bool {
	st, err := agent.LoadState(p.StateFile())
	return err == nil && st.Port > 0
}
