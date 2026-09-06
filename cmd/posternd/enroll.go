package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

func newEnrollCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Enroll a host from JSON on stdin",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !asJSON {
				return fmt.Errorf("enroll requires --json (JSON on stdin)")
			}
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			var body any
			if err := json.Unmarshal(raw, &body); err != nil {
				return fmt.Errorf("invalid JSON: %w", err)
			}
			b, _, err := apiDo(cmd, http.MethodPost, "/v1/enroll", body)
			if err != nil {
				return err
			}
			os.Stdout.Write(b)
			if len(b) > 0 && b[len(b)-1] != '\n' {
				fmt.Fprintln(os.Stdout)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "read enroll request JSON from stdin")
	return cmd
}
