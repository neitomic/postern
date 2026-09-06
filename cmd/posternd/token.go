package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/neitomic/postern/internal/auth"
	"github.com/spf13/cobra"
)

type tokenView struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Expires   int64   `json:"expires"`
	Used      bool    `json:"used"`
	Note      *string `json:"note"`
	BoundName *string `json:"bound_name"`
}

type issueResponse struct {
	OK    bool   `json:"ok"`
	Token string `json:"token"`
	tokenView
}

func newTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Issue, list, and revoke join tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newTokenIssueCmd())
	cmd.AddCommand(newTokenListCmd())
	cmd.AddCommand(newTokenRevokeCmd())
	return cmd
}

func newTokenIssueCmd() *cobra.Command {
	var ttl time.Duration
	var name, note string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Issue a one-time join token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]string{}
			if ttl != 0 {
				body["ttl"] = ttl.String()
			}
			if name != "" {
				body["name"] = name
			}
			if note != "" {
				body["note"] = note
			}
			b, _, err := apiDo(cmd, http.MethodPost, "/v1/tokens", body)
			if err != nil {
				return err
			}
			if asJSON {
				os.Stdout.Write(b)
				if len(b) > 0 && b[len(b)-1] != '\n' {
					fmt.Fprintln(os.Stdout)
				}
				return nil
			}
			var got issueResponse
			if err := json.Unmarshal(b, &got); err != nil {
				return err
			}
			fmt.Println(got.Token)
			return nil
		},
	}
	cmd.Flags().DurationVar(&ttl, "ttl", auth.DefaultTTL, "token lifetime (max 24h)")
	cmd.Flags().StringVar(&name, "name", "", "bind token to a host name")
	cmd.Flags().StringVar(&note, "note", "", "note")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newTokenListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List join tokens (never prints secrets)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, _, err := apiDo(cmd, http.MethodGet, "/v1/tokens", nil)
			if err != nil {
				return err
			}
			if asJSON {
				os.Stdout.Write(b)
				if len(b) > 0 && b[len(b)-1] != '\n' {
					fmt.Fprintln(os.Stdout)
				}
				return nil
			}
			var list []tokenView
			if err := json.Unmarshal(b, &list); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tKIND\tEXPIRES\tUSED\tBOUND_NAME\tNOTE")
			for _, t := range list {
				note := ""
				if t.Note != nil {
					note = *t.Note
				}
				bound := ""
				if t.BoundName != nil {
					bound = *t.BoundName
				}
				used := "no"
				if t.Used {
					used = "yes"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					t.ID, t.Kind, time.Unix(t.Expires, 0).UTC().Format(time.RFC3339), used, bound, note)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newTokenRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke id",
		Short: "Revoke a join token by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := apiDo(cmd, http.MethodDelete, "/v1/tokens/"+args[0], nil)
			return err
		},
	}
}
