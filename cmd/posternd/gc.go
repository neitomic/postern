package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

type gcResponse struct {
	OK            bool       `json:"ok"`
	ExpiredTokens int        `json:"expired_tokens"`
	Ports         portsAudit `json:"ports"`
}

func newGCCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Expire unused join tokens and log a ports audit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, _, err := apiDo(cmd, http.MethodPost, "/v1/gc", map[string]any{})
			if err != nil {
				return err
			}
			var got gcResponse
			if err := json.Unmarshal(b, &got); err != nil {
				return err
			}
			fmt.Printf("expired_tokens: %d\n", got.ExpiredTokens)
			printPortsAudit(os.Stdout, got.Ports)
			return nil
		},
	}
}
