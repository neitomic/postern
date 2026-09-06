package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/user"
	"runtime"
	"strconv"

	"github.com/neitomic/postern/internal/alloc"
	"github.com/neitomic/postern/internal/api"
	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/store"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the registry daemon (Linux-only Unix socket)",
		Args:  cobra.NoArgs,
		RunE:  runServe,
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	if runtime.GOOS != "linux" {
		log.Fatal("posternd serve is Linux-only")
	}
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}
	if path == "" {
		path = config.DefaultPosterndPath
	}
	cfg, err := config.LoadPosternd(path)
	if err != nil {
		return err
	}
	if v := os.Getenv("POSTERND_SOCKET"); v != "" {
		cfg.SocketPath = v
	}
	if v, _ := cmd.Flags().GetString("socket"); v != "" {
		cfg.SocketPath = v
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	u, err := user.Lookup(cfg.TunnelUser)
	if err != nil {
		return fmt.Errorf("lookup %s: %w", cfg.TunnelUser, err)
	}
	uid64, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse uid: %w", err)
	}
	gid64, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse gid: %w", err)
	}

	ln, err := api.Listen(cfg.SocketPath)
	if err != nil {
		return err
	}
	defer ln.Close()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	slog.Info("listen", "socket", cfg.SocketPath)

	srv := &api.Server{
		Store:      st,
		Config:     cfg,
		PosternUID: uint32(uid64),
		PosternGID: uint32(gid64),
		Probe:      alloc.ProcProbe(),
	}
	return srv.HTTPServer().Serve(ln)
}
