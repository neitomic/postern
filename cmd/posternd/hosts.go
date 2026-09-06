package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/neitomic/postern/internal/alloc"
	"github.com/spf13/cobra"
)

type hostView struct {
	Name           string   `json:"name"`
	LoginUser      string   `json:"login_user"`
	Port           int      `json:"port"`
	KeyFingerprint string   `json:"key_fingerprint"`
	Tags           []string `json:"tags"`
	LastSeen       *int64   `json:"last_seen"`
	Disabled       bool     `json:"disabled"`
	AgentOnline    bool     `json:"agent_online"`
	TunnelOnline   bool     `json:"tunnel_online"`
	Status         string   `json:"status"`
	Pubkey         string   `json:"pubkey,omitempty"`
}

type rmResponse struct {
	OK     bool   `json:"ok"`
	Name   string `json:"name"`
	Port   int    `json:"port"`
	Listen bool   `json:"listen"`
	PID    *int   `json:"pid,omitempty"`
	Killed bool   `json:"killed"`
}

func newHostsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hosts",
		Short: "List and administer registered hosts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newHostsListCmd())
	cmd.AddCommand(newHostsShowCmd())
	cmd.AddCommand(newHostsRmCmd())
	cmd.AddCommand(newHostsDisableCmd())
	cmd.AddCommand(newHostsEnableCmd())
	cmd.AddCommand(newHostsRenameCmd())
	cmd.AddCommand(newHostsRekeyCmd())
	return cmd
}

func newHostsListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered hosts and LISTEN/heartbeat status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, _, err := apiDo(cmd, http.MethodGet, "/v1/hosts", nil)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSONOut(b)
			}
			var list []hostView
			if err := json.Unmarshal(b, &list); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tUSER\tPORT\tTUNNEL\tAGENT\tSTATUS\tLAST_SEEN\tTAGS")
			now := time.Now()
			for _, h := range list {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
					h.Name, h.LoginUser, h.Port,
					upDown(h.TunnelOnline), upDown(h.AgentOnline),
					h.Status, fmtLastSeen(h.LastSeen, now), strings.Join(h.Tags, ","))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newHostsShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show name",
		Short: "Show one host, including the re-serialized pubkey",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, _, err := apiDo(cmd, http.MethodGet, "/v1/hosts/"+url.PathEscape(args[0]), nil)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSONOut(b)
			}
			var h hostView
			if err := json.Unmarshal(b, &h); err != nil {
				return err
			}
			fmt.Printf("name: %s\n", h.Name)
			fmt.Printf("login_user: %s\n", h.LoginUser)
			fmt.Printf("port: %d\n", h.Port)
			fmt.Printf("key_fingerprint: %s\n", h.KeyFingerprint)
			fmt.Printf("tags: %s\n", strings.Join(h.Tags, ","))
			fmt.Printf("last_seen: %s\n", fmtLastSeen(h.LastSeen, time.Now()))
			fmt.Printf("disabled: %t\n", h.Disabled)
			fmt.Printf("agent_online: %t\n", h.AgentOnline)
			fmt.Printf("tunnel_online: %t\n", h.TunnelOnline)
			fmt.Printf("status: %s\n", h.Status)
			fmt.Printf("pubkey: %s\n", h.Pubkey)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newHostsRmCmd() *cobra.Command {
	var killListen bool
	cmd := &cobra.Command{
		Use:   "rm name",
		Short: "Delete a host and re-render authorized_keys",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path := "/v1/hosts/" + url.PathEscape(name)
			if killListen {
				path += "?kill_listen=1"
			}
			b, _, err := apiDo(cmd, http.MethodDelete, path, nil)
			if err != nil {
				return err
			}
			var got rmResponse
			if err := json.Unmarshal(b, &got); err != nil {
				return err
			}
			fmt.Printf("removed %s (port %d)\n", got.Name, got.Port)
			if !got.Listen {
				return nil
			}
			runbook := listenRunbook(got.Port, got.PID)
			fmt.Fprintf(os.Stderr, "warning: port %d still LISTEN\n%s\n", got.Port, runbook)
			if !killListen {
				return nil
			}
			if os.Geteuid() != 0 {
				return exitCodeError{error: fmt.Errorf("port %d still LISTEN; --kill-listen requires root", got.Port), code: 2}
			}
			if err := alloc.KillListenPort(got.Port); err != nil {
				return exitCodeError{error: fmt.Errorf("%w\n%s", err, runbook), code: 2}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&killListen, "kill-listen", false, "after render, kill the sshd child holding the reverse-forward (requires root)")
	return cmd
}

func newHostsDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable name",
		Short: "Disable a host (keep port, omit from authorized_keys)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := apiDo(cmd, http.MethodPost, "/v1/hosts/"+url.PathEscape(args[0])+"/disable", map[string]any{})
			return err
		},
	}
}

func newHostsEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable name",
		Short: "Enable a disabled host and re-render authorized_keys",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := apiDo(cmd, http.MethodPost, "/v1/hosts/"+url.PathEscape(args[0])+"/enable", map[string]any{})
			return err
		},
	}
}

func newHostsRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename old new",
		Short: "Rename a host and re-render authorized_keys",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := apiDo(cmd, http.MethodPost, "/v1/hosts/"+url.PathEscape(args[0])+"/rename", map[string]string{
				"new_name": args[1],
			})
			return err
		},
	}
}

func newHostsRekeyCmd() *cobra.Command {
	var oldFP, pubkey string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "rekey name",
		Short: "Replace a host pubkey, keeping the name and port",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if pubkey == "" {
				raw, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				pubkey = strings.TrimSpace(string(raw))
			}
			if pubkey == "" {
				return fmt.Errorf("rekey requires --pubkey or a pubkey on stdin")
			}
			b, _, err := apiDo(cmd, http.MethodPost, "/v1/hosts/"+url.PathEscape(args[0])+"/rekey", map[string]string{
				"old_fingerprint": oldFP,
				"pubkey":          pubkey,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSONOut(b)
			}
			var h hostView
			if err := json.Unmarshal(b, &h); err != nil {
				return err
			}
			fmt.Printf("%s rekeyed fingerprint %s port %d\n", h.Name, h.KeyFingerprint, h.Port)
			return nil
		},
	}
	cmd.Flags().StringVar(&oldFP, "old-fingerprint", "", "current SHA256: fingerprint (required)")
	cmd.Flags().StringVar(&pubkey, "pubkey", "", "new ssh-ed25519 public key")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	_ = cmd.MarkFlagRequired("old-fingerprint")
	return cmd
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
	d := now.Sub(time.Unix(*ts, 0)).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return d.String() + " ago"
}

func listenRunbook(port int, pid *int) string {
	hint := "<pid>"
	if pid != nil {
		hint = strconv.Itoa(*pid)
	}
	return fmt.Sprintf("sudo ss -ltnp sport = :%d\n# identify the sshd child of the reverse forward, not systemd's sshd\nsudo kill %s\nposternd ports audit", port, hint)
}

func writeJSONOut(b []byte) error {
	os.Stdout.Write(b)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		fmt.Fprintln(os.Stdout)
	}
	return nil
}
