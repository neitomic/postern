package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/sshconfig"
	"github.com/neitomic/postern/internal/version"
	"github.com/spf13/cobra"
)

const sshBin = "/usr/bin/ssh"

var (
	fetchHostsJSON = fetchHostsJSONDefault
	execve         = execveDefault
	now            = time.Now
)

func newLSCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List registered hosts (TUNNEL and AGENT)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := fetchHostsJSON()
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSONOut(cmd.OutOrStdout(), raw)
			}
			hosts, err := parseHosts(raw)
			if err != nil {
				return err
			}
			return printLS(cmd.OutOrStdout(), hosts, now())
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newSSHConfigCmd() *cobra.Command {
	var write bool
	var path string
	cmd := &cobra.Command{
		Use:   "ssh-config",
		Short: "Print or write managed ssh_config Host stanzas",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if path != "" && !write {
				return fmt.Errorf("--path requires --write")
			}
			cfg, hosts, err := loadClientAndHosts()
			if err != nil {
				return err
			}
			body, err := renderSSHConfig(cfg, hosts)
			if err != nil {
				return err
			}
			if !write {
				_, err := io.WriteString(cmd.OutOrStdout(), body)
				return err
			}
			outPath := path
			if outPath == "" {
				outPath, err = config.UserSSHConfigPath()
				if err != nil {
					return err
				}
			}
			return sshconfig.WriteFile(outPath, body)
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "replace the managed block in ~/.ssh/config (or --path)")
	cmd.Flags().StringVar(&path, "path", "", "ssh_config path (requires --write)")
	return cmd
}

func newSSHCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "ssh name [-- ssh-args...]",
		Short:              "ssh via generated jump+host config",
		DisableFlagParsing: true,
		Args:               cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "-h" || args[0] == "--help" {
				return cmd.Help()
			}
			return runSSHWrapper(cmd.ErrOrStderr(), args)
		},
	}
}

func runSSHWrapper(stderr io.Writer, args []string) error {
	name := args[0]
	extra := args[1:]
	if len(extra) > 0 && extra[0] == "--" {
		extra = extra[1:]
	}
	cfg, hosts, err := loadClientAndHosts()
	if err != nil {
		return err
	}
	h, ok := lookupHost(hosts, name)
	if !ok || h.Disabled {
		return exitCodeError{error: fmt.Errorf("unknown host %q", name), code: 2}
	}
	if h.Status == "offline" || h.Status == "degraded" {
		fmt.Fprintf(stderr, "warning: host %s is %s\n", name, h.Status)
	}
	body, err := renderSSHConfig(cfg, hosts)
	if err != nil {
		return err
	}
	path, err := writeClientSSHConfig(body)
	if err != nil {
		return err
	}
	return execve(sshWrapperArgv(path, name, extra))
}

func sshWrapperArgv(file, name string, extra []string) []string {
	opts, cmd := splitSSHExtras(extra)
	argv := []string{sshBin, "-F", file}
	argv = append(argv, opts...)
	argv = append(argv, name)
	return append(argv, cmd...)
}

// splitSSHExtras puts ssh(1) client flags before the destination so
// `postern ssh macbook -- -v` is verbose, not a remote command named -v.
func splitSSHExtras(extra []string) (opts, cmd []string) {
	takesArg := map[byte]bool{
		'B': true, 'b': true, 'c': true, 'D': true, 'E': true, 'e': true,
		'F': true, 'I': true, 'i': true, 'J': true, 'L': true, 'l': true,
		'm': true, 'O': true, 'o': true, 'P': true, 'p': true, 'R': true,
		'S': true, 'W': true, 'w': true,
	}
	i := 0
	for i < len(extra) {
		a := extra[i]
		if a == "--" {
			return opts, extra[i+1:]
		}
		if a == "" || a[0] != '-' || a == "-" {
			return opts, extra[i:]
		}
		opts = append(opts, a)
		if len(a) == 2 && takesArg[a[1]] {
			if i+1 < len(extra) {
				i++
				opts = append(opts, extra[i])
			}
		}
		i++
	}
	return opts, nil
}

func loadClientAndHosts() (config.Client, []sshconfig.Host, error) {
	cfg, err := loadClient()
	if err != nil {
		return config.Client{}, nil, err
	}
	raw, err := fetchHostsJSON()
	if err != nil {
		return config.Client{}, nil, err
	}
	hosts, err := parseHosts(raw)
	if err != nil {
		return config.Client{}, nil, err
	}
	return cfg, hosts, nil
}

func loadClient() (config.Client, error) {
	path, err := config.ClientPath()
	if err != nil {
		return config.Client{}, err
	}
	return config.LoadClient(path)
}

func renderSSHConfig(cfg config.Client, hosts []sshconfig.Host) (string, error) {
	user, host, port := cfg.Jump()
	if user == "" || host == "" {
		return "", fmt.Errorf("jump_user/jump_host unset (set them or server = user@host)")
	}
	jump := sshconfig.Jump{
		User:         user,
		Host:         host,
		Port:         port,
		IdentityFile: cfg.IdentityFile,
	}
	return sshconfig.Render(jump, hosts, version.Version, now().UTC())
}

func writeClientSSHConfig(body string) (string, error) {
	path, err := config.ClientSSHConfigPath()
	if err != nil {
		return "", err
	}
	if err := sshconfig.WritePrivate(path, body); err != nil {
		return "", err
	}
	return path, nil
}

func fetchHostsJSONDefault() ([]byte, error) {
	cfg, err := loadClient()
	if err != nil {
		return nil, err
	}
	if cfg.Server == "" {
		return nil, fmt.Errorf("server is not set in config")
	}
	known, err := config.KnownHostsPath()
	if err != nil {
		return nil, err
	}
	path := cfg.PosterndPath
	if path == "" {
		path = "/usr/bin/posternd"
	}
	args := controlSSHArgs(cfg, known, path, "hosts", "list", "--json")
	return runSSH(args)
}

func controlSSHArgs(cfg config.Client, knownHosts string, remote ...string) []string {
	args := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "ForwardAgent=no",
	}
	if cfg.IdentityFile != "" {
		args = append(args, "-o", "IdentityFile="+cfg.IdentityFile, "-o", "IdentitiesOnly=yes")
	}
	args = append(args, controlSSHDest(cfg)...)
	args = append(args, remote...)
	return args
}

func controlSSHDest(cfg config.Client) []string {
	user, host, port, err := config.ParseServer(cfg.Server)
	if err != nil || host == "" {
		host = cfg.Server
	}
	dest := host
	if user != "" {
		dest = user + "@" + host
	}
	if port > 0 && port != 22 {
		return []string{"-p", strconv.Itoa(port), dest}
	}
	return []string{dest}
}

func runSSH(args []string) ([]byte, error) {
	cmd := exec.Command(sshBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}

func execveDefault(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty argv")
	}
	return syscall.Exec(argv[0], argv, os.Environ())
}

func parseHosts(b []byte) ([]sshconfig.Host, error) {
	var hosts []sshconfig.Host
	if err := json.Unmarshal(b, &hosts); err != nil {
		return nil, fmt.Errorf("hosts list JSON: %w", err)
	}
	return hosts, nil
}

func lookupHost(hosts []sshconfig.Host, name string) (sshconfig.Host, bool) {
	for _, h := range hosts {
		if h.Name == name {
			return h, true
		}
	}
	return sshconfig.Host{}, false
}

func printLS(w io.Writer, hosts []sshconfig.Host, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tUSER\tPORT\tTUNNEL\tAGENT\tSTATUS\tLAST_SEEN\tTAGS")
	for _, h := range hosts {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
			h.Name, h.LoginUser, h.Port,
			upDown(h.TunnelOnline), upDown(h.AgentOnline),
			h.Status, fmtLastSeen(h.LastSeen, now), strings.Join(h.Tags, ","))
	}
	return tw.Flush()
}

func upDown(ok bool) string {
	if ok {
		return "up"
	}
	return "down"
}

func fmtLastSeen(ts *int64, now time.Time) string {
	if ts == nil {
		return "-"
	}
	d := now.Sub(time.Unix(*ts, 0))
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func writeJSONOut(w io.Writer, b []byte) error {
	if _, err := w.Write(b); err != nil {
		return err
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}
