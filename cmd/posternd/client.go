package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/neitomic/postern/internal/config"
	"github.com/spf13/cobra"
)

type apiError struct {
	OK      bool   `json:"ok"`
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (e apiError) Error() string {
	if e.Message != "" && e.Code != "" {
		return e.Code + ": " + e.Message
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return "request failed"
}

func loadPosterndCfg(cmd *cobra.Command) (config.Posternd, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return config.Posternd{}, err
	}
	if path == "" {
		path = config.DefaultPosterndPath
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return config.DefaultPosternd(), nil
		}
		return config.Posternd{}, err
	}
	return config.LoadPosternd(path)
}

func socketPath(cmd *cobra.Command, cfg config.Posternd) string {
	if v, _ := cmd.Flags().GetString("socket"); v != "" {
		return v
	}
	if v := os.Getenv("POSTERND_SOCKET"); v != "" {
		return v
	}
	return cfg.SocketPath
}

func unixClient(socket string) *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
}

func apiDo(cmd *cobra.Command, method, path string, body any) ([]byte, int, error) {
	cfg, err := loadPosterndCfg(cmd)
	if err != nil {
		return nil, 0, err
	}
	sock := socketPath(cmd, cfg)
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(cmd.Context(), method, "http://localhost"+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Host = "localhost"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := unixClient(sock).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		var ae apiError
		if json.Unmarshal(b, &ae) == nil && (ae.Code != "" || ae.Message != "") {
			return b, resp.StatusCode, ae
		}
		return b, resp.StatusCode, fmt.Errorf("HTTP %s", resp.Status)
	}
	return b, resp.StatusCode, nil
}
