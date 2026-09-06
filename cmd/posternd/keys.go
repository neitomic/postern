package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

func newAuthorizedKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "authorized-keys",
		Short: "Render the tunnel user authorized_keys file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAuthorizedKeysRenderCmd())
	return cmd
}

func newAuthorizedKeysRenderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "render",
		Short: "Rewrite authorized_keys from the registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, _, err := apiDo(cmd, http.MethodPost, "/v1/authorized-keys/render", map[string]any{})
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
}
