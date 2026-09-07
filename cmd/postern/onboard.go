package main

import (
	"fmt"

	"github.com/neitomic/postern/internal/agent"
	"github.com/neitomic/postern/internal/config"
	"github.com/spf13/cobra"
)

func newOnboardCmd() *cobra.Command {
	var (
		server        string
		name          string
		loginUser     string
		localSSHPort  int
		token         string
		applyResponse string
		submit        bool
		force         bool
		acceptHost    bool
		noInstall     bool
	)
	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Configure, enroll, and start the agent on this machine",
		Long: `One command to bring this machine onto the VPS.

Walkthrough: ONBOARDING.md

  # this machine can already ssh USER@vps (laptop):
  postern onboard --server debian@vps --name macbook --token TOKEN --submit

  # headless box (no admin SSH here):
  postern onboard --server debian@vps --name nuc --token TOKEN
  # then on the laptop, copy files as printed; finish with:
  postern onboard --apply-response enroll-response.json

Issues a tunnel key, writes config, pins the VPS host key, enrolls, and
enables the user agent (autostart). --submit SSHes to the VPS as admin
from this machine (copies admin authority; not for a nuc/pi).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnboard(cmd, onboardOpts{
				server:        server,
				name:          name,
				loginUser:     loginUser,
				localSSHPort:  localSSHPort,
				token:         token,
				applyResponse: applyResponse,
				submit:        submit,
				force:         force,
				acceptHost:    acceptHost,
				noInstall:     noInstall,
			})
		},
		SilenceUsage: true,
	}
	cmd.Flags().StringVar(&server, "server", "", "admin SSH target USER@HOST")
	cmd.Flags().StringVar(&name, "name", "", "name for this machine (postern ls / ssh NAME)")
	cmd.Flags().StringVar(&loginUser, "login-user", "", "login user on this machine (default: this account)")
	cmd.Flags().IntVar(&localSSHPort, "local-ssh-port", 22, "local sshd port for the reverse forward")
	cmd.Flags().StringVar(&token, "token", "", "one-time join token from posternd token issue")
	cmd.Flags().StringVar(&applyResponse, "apply-response", "", "finish split enroll with the laptop's response JSON")
	cmd.Flags().BoolVar(&submit, "submit", false, "enroll over admin SSH from this machine (not the headless path)")
	cmd.Flags().BoolVar(&force, "force", false, "write a new enroll request even if already enrolled")
	cmd.Flags().BoolVar(&acceptHost, "accept-host-key", true, "pin VPS host keys via ssh-keyscan")
	cmd.Flags().BoolVar(&noInstall, "no-install", false, "skip copying the binary and enabling the agent")
	return cmd
}

type onboardOpts struct {
	server, name, loginUser, token, applyResponse string
	localSSHPort                                  int
	submit, force, acceptHost, noInstall          bool
}

func runOnboard(cmd *cobra.Command, o onboardOpts) error {
	if o.applyResponse != "" {
		if o.token != "" || o.submit || o.force {
			return fmt.Errorf("--apply-response cannot be combined with --token, --submit, or --force")
		}
		if err := runApplyResponse(cmd, o.applyResponse); err != nil {
			return err
		}
		return finishOnboard(cmd, o.noInstall, true)
	}

	p, err := agent.DefaultPaths()
	if err != nil {
		return err
	}
	existing, err := loadOrDefaultClient(p)
	if err != nil {
		return err
	}
	if o.server == "" {
		o.server = existing.Server
	}
	if o.name == "" {
		o.name = existing.Name
	}

	if o.server == "" && o.token == "" {
		return fmt.Errorf("need --server USER@HOST and --name NAME (see ONBOARDING.md)")
	}

	if err := runInit(cmd, o.server, o.name, o.loginUser, o.localSSHPort, o.acceptHost && o.server != ""); err != nil {
		return err
	}
	cfg, err := config.LoadClient(p.ConfigFile())
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "config %s  server=%s  name=%s  login_user=%s\n", p.ConfigFile(), cfg.Server, cfg.Name, cfg.LoginUser)

	if o.token == "" {
		fmt.Fprint(cmd.ErrOrStderr(), `
Next — issue a token on the VPS (or from a laptop that can ssh there):

  ssh -T USER@vps /usr/bin/posternd token issue --ttl 15m --name `+cfg.Name+`

Then on this machine:

  postern onboard --token psn_join_…
  # or, if this machine can ssh USER@vps:
  postern onboard --submit --token psn_join_…

`)
		return finishOnboard(cmd, o.noInstall, false)
	}

	if cfg.Name == "" {
		return fmt.Errorf("--name is required (or postern config set name …)")
	}

	if o.submit {
		if err := runSubmit(cmd, o.token, o.force); err != nil {
			return err
		}
		return finishOnboard(cmd, o.noInstall, true)
	}
	if err := runJoinToken(cmd, o.token, o.force); err != nil {
		return err
	}
	return finishOnboard(cmd, o.noInstall, false)
}

func finishOnboard(cmd *cobra.Command, noInstall, enrolled bool) error {
	if !noInstall {
		if err := runInstall(cmd, "", false, false, true); err != nil {
			return err
		}
	}
	if enrolled {
		p, err := agent.DefaultPaths()
		if err != nil {
			return err
		}
		st, err := agent.LoadState(p.StateFile())
		if err == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "onboarded %s  port %d\n", st.Name, st.Port)
			fmt.Fprintf(cmd.OutOrStdout(), "from a laptop:  postern ls && postern ssh %s\n", st.Name)
		}
		return nil
	}
	if noInstall {
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "agent enabled; it waits until enroll finishes (ONBOARDING.md).")
	return nil
}
