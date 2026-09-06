package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/neitomic/postern/internal/agent"
	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/names"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or set client config (~/.config/postern/config.toml)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow(cmd)
		},
		SilenceUsage: true,
	}
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigAcceptHostKeyCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print config.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow(cmd)
		},
		SilenceUsage: true,
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get KEY",
		Short: "Print one config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := agent.DefaultPaths()
			if err != nil {
				return err
			}
			cfg, err := loadOrDefaultClient(p)
			if err != nil {
				return err
			}
			val, err := configValue(cfg, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), val)
			return nil
		},
		SilenceUsage: true,
	}
}

func newConfigSetCmd() *cobra.Command {
	var acceptHost bool
	cmd := &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Set one config key and write config.toml",
		Long: `Keys: server, name, login-user, local-ssh-port, identity-file, posternd-path,
jump-user, jump-host, jump-port.

Creates XDG dirs and a tunnel key if missing. --accept-host-key pins the VPS
host key after setting server (or if server is already in the file).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigSet(cmd, args[0], args[1], acceptHost)
		},
		SilenceUsage: true,
	}
	cmd.Flags().BoolVar(&acceptHost, "accept-host-key", false, "pin VPS host keys into Postern known_hosts via ssh-keyscan")
	return cmd
}

func newConfigAcceptHostKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accept-host-key",
		Short: "Pin the VPS host key (requires config server)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := agent.DefaultPaths()
			if err != nil {
				return err
			}
			cfg, err := config.LoadClient(p.ConfigFile())
			if err != nil {
				return fmt.Errorf("client config not found (run postern config set server …): %w", err)
			}
			return acceptHostKey(p, cfg)
		},
		SilenceUsage: true,
	}
}

func runConfigShow(cmd *cobra.Command) error {
	p, err := agent.DefaultPaths()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(p.ConfigFile())
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("client config not found (run postern config set server …): %s", p.ConfigFile())
		}
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "# %s\n", p.ConfigFile())
	fmt.Fprint(cmd.OutOrStdout(), string(raw))
	if !strings.HasSuffix(string(raw), "\n") {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func runConfigSet(cmd *cobra.Command, key, value string, acceptHost bool) error {
	p, err := agent.DefaultPaths()
	if err != nil {
		return err
	}
	if err := p.Mkdir(); err != nil {
		return err
	}
	cfg, err := loadOrDefaultClient(p)
	if err != nil {
		return err
	}
	if err := applyConfigKey(&cfg, key, value); err != nil {
		return err
	}
	if err := agent.EnsureKey(p.IdentityFile(), agent.KeyComment(cfg.Name)); err != nil {
		return err
	}
	if err := cfg.Save(p.ConfigFile()); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s\n", normalizeConfigKey(key), displayConfigValue(cfg, key))
	if acceptHost {
		return acceptHostKey(p, cfg)
	}
	return nil
}

func loadOrDefaultClient(p agent.Paths) (config.Client, error) {
	cfg, err := config.LoadClient(p.ConfigFile())
	if err == nil {
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return config.Client{}, err
	}
	cfg = config.DefaultClient()
	if u := defaultLoginUser(); u != "" {
		cfg.LoginUser = u
	}
	return cfg, nil
}

func acceptHostKey(p agent.Paths, cfg config.Client) error {
	if cfg.Server == "" {
		return fmt.Errorf("--accept-host-key requires server (postern config set server USER@HOST)")
	}
	_, host, port, err := config.ParseServer(cfg.Server)
	if err != nil {
		return err
	}
	return agent.AcceptHostKey(p.KnownHosts(), host, port)
}

func normalizeConfigKey(key string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(key)), "-", "_")
}

func applyConfigKey(cfg *config.Client, key, value string) error {
	value = strings.TrimSpace(value)
	switch normalizeConfigKey(key) {
	case "server":
		if _, _, _, err := config.ParseServer(value); err != nil {
			return err
		}
		cfg.Server = value
	case "name":
		value = strings.ToLower(value)
		if err := names.Valid(value); err != nil {
			return err
		}
		cfg.Name = value
	case "login_user":
		value = strings.ToLower(value)
		if err := names.ValidLoginUser(value); err != nil {
			return err
		}
		cfg.LoginUser = value
	case "local_ssh_port":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("local-ssh-port %q out of range", value)
		}
		cfg.LocalSSHPort = n
	case "identity_file":
		cfg.IdentityFile = value
	case "posternd_path":
		if value == "" {
			return fmt.Errorf("posternd-path must not be empty")
		}
		cfg.PosterndPath = value
	case "jump_user":
		cfg.JumpUser = value
	case "jump_host":
		if err := names.ValidHostname(value); err != nil {
			return err
		}
		cfg.JumpHost = value
	case "jump_port":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("jump-port %q out of range", value)
		}
		cfg.JumpPort = n
	default:
		return fmt.Errorf("unknown config key %q (server, name, login-user, local-ssh-port, identity-file, posternd-path, jump-user, jump-host, jump-port)", key)
	}
	return nil
}

func configValue(cfg config.Client, key string) (string, error) {
	switch normalizeConfigKey(key) {
	case "server":
		return cfg.Server, nil
	case "name":
		return cfg.Name, nil
	case "login_user":
		return cfg.LoginUser, nil
	case "local_ssh_port":
		return strconv.Itoa(cfg.LocalSSHPort), nil
	case "identity_file":
		return cfg.IdentityFile, nil
	case "posternd_path":
		return cfg.PosterndPath, nil
	case "jump_user":
		return cfg.JumpUser, nil
	case "jump_host":
		return cfg.JumpHost, nil
	case "jump_port":
		return strconv.Itoa(cfg.JumpPort), nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func displayConfigValue(cfg config.Client, key string) string {
	v, err := configValue(cfg, key)
	if err != nil {
		return ""
	}
	return v
}
