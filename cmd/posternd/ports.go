package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

type portRow struct {
	Name string `json:"name,omitempty"`
	Port int    `json:"port"`
	PID  *int   `json:"pid,omitempty"`
}

type portsAudit struct {
	DBOwnedNotListening []portRow `json:"db_owned_not_listening"`
	ListeningNotDBOwned []portRow `json:"listening_not_db_owned"`
	Both                []portRow `json:"both"`
}

func newPortsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ports",
		Short: "Audit DB-owned vs LISTEN ports",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newPortsAuditCmd())
	return cmd
}

func newPortsAuditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit",
		Short: "List DB-owned vs LISTEN ports (pids best-effort)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, _, err := apiDo(cmd, http.MethodGet, "/v1/ports", nil)
			if err != nil {
				return err
			}
			var got portsAudit
			if err := json.Unmarshal(b, &got); err != nil {
				return err
			}
			printPortsAudit(os.Stdout, got)
			if missingPID(got) && os.Geteuid() != 0 {
				fmt.Fprintln(os.Stderr, "pids unreadable without root")
				fmt.Fprintln(os.Stderr, "sudo ss -ltnp")
				fmt.Fprintln(os.Stderr, "# identify the sshd child of the reverse forward, not systemd's sshd")
			}
			return nil
		},
	}
}

func printPortsAudit(w io.Writer, a portsAudit) {
	fmt.Fprintln(w, "DB-owned, not listening:")
	if len(a.DBOwnedNotListening) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, r := range a.DBOwnedNotListening {
		fmt.Fprintf(w, "  %s\t%d\n", r.Name, r.Port)
	}
	fmt.Fprintln(w, "Listening, not DB-owned:")
	if len(a.ListeningNotDBOwned) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, r := range a.ListeningNotDBOwned {
		fmt.Fprintf(w, "  %d%s\n", r.Port, pidSuffix(r.PID))
	}
	fmt.Fprintln(w, "Both:")
	if len(a.Both) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, r := range a.Both {
		fmt.Fprintf(w, "  %s\t%d%s\n", r.Name, r.Port, pidSuffix(r.PID))
	}
}

func pidSuffix(pid *int) string {
	if pid == nil {
		return ""
	}
	return "\tpid=" + strconv.Itoa(*pid)
}

func missingPID(a portsAudit) bool {
	for _, r := range a.ListeningNotDBOwned {
		if r.PID == nil {
			return true
		}
	}
	for _, r := range a.Both {
		if r.PID == nil {
			return true
		}
	}
	return false
}
