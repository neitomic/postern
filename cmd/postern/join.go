package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/neitomic/postern/internal/agent"
	"github.com/spf13/cobra"
)

func newJoinCmd() *cobra.Command {
	var (
		token         string
		applyResponse string
		submit        bool
		force         bool
	)
	cmd := &cobra.Command{
		Use:   "join",
		Short: "Write an enroll request or bind an enroll response",
		Long: `Split enroll is the default path.

  postern join --token TOKEN
      Write enroll-request.json and print it. Does not SSH as admin.

  Copy that file to the operator laptop, run postern enroll-machine,
  copy the response back, then:

  postern join --apply-response FILE
      Bind ok/name/fingerprint/port and write state.json + ssh_config.

  postern join --submit --token TOKEN
      Optional convenience: enroll over admin SSH from this machine.
      This copies admin SSH authority onto this box and is not the
      headless path. Never uses ssh-agent forwarding (-A).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case applyResponse != "":
				if token != "" || submit || force {
					return fmt.Errorf("--apply-response cannot be combined with --token, --submit, or --force")
				}
				return runApplyResponse(cmd, applyResponse)
			case submit:
				if token == "" {
					return fmt.Errorf("--submit requires --token")
				}
				return runSubmit(cmd, token, force)
			case token != "":
				return runJoinToken(cmd, token, force)
			default:
				return fmt.Errorf("join requires --token or --apply-response")
			}
		},
		SilenceUsage: true,
	}
	cmd.Flags().StringVar(&token, "token", "", "one-time join token")
	cmd.Flags().StringVar(&applyResponse, "apply-response", "", "enroll response JSON file")
	cmd.Flags().BoolVar(&submit, "submit", false, "submit enroll over admin SSH from this machine (copies admin authority; not the headless path)")
	cmd.Flags().BoolVar(&force, "force", false, "write a new enroll request even if state.json exists")
	return cmd
}

func runJoinToken(cmd *cobra.Command, token string, force bool) error {
	cfg, p, err := loadConfigured()
	if err != nil {
		return err
	}
	_, raw, err := agent.WriteEnrollRequest(p, cfg, token, force)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), string(raw))
	fmt.Fprintf(cmd.ErrOrStderr(), `
Wrote %s. This machine does not SSH as admin.

Next:
  1. Copy the JSON above to the operator laptop
  2. On the laptop: postern enroll-machine enroll-request.json
  3. Copy the response JSON back here
  4. postern join --apply-response enroll-response.json

Optional: postern join --submit --token … copies admin SSH authority onto this machine and is not the headless path.
`, p.EnrollRequest())
	return nil
}

func runApplyResponse(cmd *cobra.Command, path string) error {
	cfg, p, err := loadConfigured()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	st, err := agent.ApplyResponse(p, cfg, raw)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "enrolled %s port %d\n", st.Name, st.Port)
	return nil
}

func runSubmit(cmd *cobra.Command, token string, force bool) error {
	cfg, p, err := loadConfigured()
	if err != nil {
		return err
	}
	if _, _, err := agent.WriteEnrollRequest(p, cfg, token, force); err != nil {
		return err
	}
	sshBin, err := agent.LookPathSSH()
	if err != nil {
		return err
	}
	probeArgs, err := agent.SubmitProbeArgs(cfg, p.KnownHosts())
	if err != nil {
		return err
	}
	probe := exec.CommandContext(cmd.Context(), sshBin, probeArgs...)
	if out, err := probe.CombinedOutput(); err != nil {
		return fmt.Errorf("admin SSH to %s failed (join --submit copies admin authority onto this machine; use split enroll instead): %w: %s", cfg.Server, err, bytes.TrimSpace(out))
	}

	f, err := os.Open(p.EnrollRequest())
	if err != nil {
		return err
	}
	defer f.Close()
	enrollArgs, err := agent.EnrollMachineArgs(cfg, p.KnownHosts())
	if err != nil {
		return err
	}
	sshCmd := exec.CommandContext(cmd.Context(), sshBin, enrollArgs...)
	sshCmd.Stdin = f
	var stdout, stderr bytes.Buffer
	sshCmd.Stdout = &stdout
	sshCmd.Stderr = &stderr
	if err := sshCmd.Run(); err != nil {
		msg := bytes.TrimSpace(stderr.Bytes())
		if len(msg) == 0 {
			msg = bytes.TrimSpace(stdout.Bytes())
		}
		return fmt.Errorf("enroll via ssh: %w: %s", err, msg)
	}
	st, err := agent.ApplyResponse(p, cfg, stdout.Bytes())
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "enrolled %s port %d\n", st.Name, st.Port)
	return nil
}
