package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/neitomic/postern/internal/agent"
	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/names"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var (
		server       string
		name         string
		loginUser    string
		localSSHPort int
		acceptHost   bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create XDG dirs, tunnel key, and config.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, server, name, loginUser, localSSHPort, acceptHost)
		},
		SilenceUsage: true,
	}
	cmd.Flags().StringVar(&server, "server", "", "admin SSH target USER@HOST")
	cmd.Flags().StringVar(&name, "name", "", "machine name")
	cmd.Flags().StringVar(&loginUser, "login-user", "", "login user on this machine")
	cmd.Flags().IntVar(&localSSHPort, "local-ssh-port", 22, "local sshd port for RemoteForward")
	cmd.Flags().BoolVar(&acceptHost, "accept-host-key", false, "pin VPS host keys into Postern known_hosts via ssh-keyscan")
	return cmd
}

func runInit(cmd *cobra.Command, server, name, loginUser string, localSSHPort int, acceptHost bool) error {
	p, err := agent.DefaultPaths()
	if err != nil {
		return err
	}
	if err := p.Mkdir(); err != nil {
		return err
	}

	cfg := config.DefaultClient()
	loaded := false
	if existing, err := config.LoadClient(p.ConfigFile()); err == nil {
		cfg = existing
		loaded = true
	} else if !os.IsNotExist(err) {
		return err
	}

	server = strings.TrimSpace(server)
	if server != "" {
		if _, _, _, err := config.ParseServer(server); err != nil {
			return err
		}
		cfg.Server = server
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name != "" {
		if err := names.Valid(name); err != nil {
			return err
		}
		cfg.Name = name
	}
	loginUser = strings.ToLower(strings.TrimSpace(loginUser))
	if loginUser != "" {
		if err := names.ValidLoginUser(loginUser); err != nil {
			return err
		}
		cfg.LoginUser = loginUser
	} else if !loaded {
		if u := defaultLoginUser(); u != "" {
			cfg.LoginUser = u
		}
	}
	if cmd.Flags().Changed("local-ssh-port") || !loaded {
		if localSSHPort < 1 || localSSHPort > 65535 {
			return fmt.Errorf("local-ssh-port %d out of range", localSSHPort)
		}
		cfg.LocalSSHPort = localSSHPort
	}

	if err := agent.EnsureKey(p.IdentityFile(), agent.KeyComment(cfg.Name)); err != nil {
		return err
	}
	if err := cfg.Save(p.ConfigFile()); err != nil {
		return err
	}

	if acceptHost {
		if cfg.Server == "" {
			return fmt.Errorf("--accept-host-key requires --server")
		}
		_, host, port, err := config.ParseServer(cfg.Server)
		if err != nil {
			return err
		}
		if err := agent.AcceptHostKey(p.KnownHosts(), host, port); err != nil {
			return err
		}
	}
	return nil
}
