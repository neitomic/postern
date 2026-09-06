package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/neitomic/postern/internal/agent"
	"github.com/spf13/cobra"
)

func newEnrollMachineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enroll-machine file",
		Short: "Submit enroll-request.json over admin SSH from this laptop",
		Long: `Read FILE (enroll-request.json) and run:

  ssh -T -o BatchMode=yes ${server} ${posternd_path} enroll --json

Admin SSH stays on the operator laptop. Does not forward the agent (-A).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnrollMachine(cmd, args[0])
		},
		SilenceUsage: true,
	}
}

func runEnrollMachine(cmd *cobra.Command, file string) error {
	cfg, p, err := loadConfigured()
	if err != nil {
		return err
	}
	if cfg.Server == "" {
		return fmt.Errorf("server is empty (run postern init --server USER@HOST)")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	sshBin, err := agent.LookPathSSH()
	if err != nil {
		return err
	}
	args, err := agent.EnrollMachineArgs(cfg, p.KnownHosts())
	if err != nil {
		return err
	}
	sshCmd := exec.CommandContext(cmd.Context(), sshBin, args...)
	sshCmd.Stdin = f
	sshCmd.Stdout = cmd.OutOrStdout()
	sshCmd.Stderr = cmd.ErrOrStderr()
	return sshCmd.Run()
}
