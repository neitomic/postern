package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/neitomic/postern/internal/config"
)

const (
	heartbeatEvery  = 30 * time.Second
	heartbeatJitter = 2 * time.Second
	heartbeatWait   = 20 * time.Second
)

// RunOptions configures agent run. Zero value is the production path.
type RunOptions struct {
	Autossh   string
	SSH       string
	Heartbeat time.Duration
	Jitter    time.Duration
	LookPath  func(string) (string, error)

	writeSSHConfigs func(Paths, State, config.Client) error
}

func Run(ctx context.Context, p Paths, opts RunOptions) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, st, err := loadEnrolled(p)
	if err != nil {
		return err
	}
	write := opts.writeSSHConfigs
	if write == nil {
		write = WriteSSHConfigs
	}
	if err := write(p, st, cfg); err != nil {
		return err
	}
	if err := checkRemoteForward(p.SSHConfig(), st.Port); err != nil {
		return err
	}

	autossh := opts.Autossh
	if autossh == "" {
		if opts.LookPath != nil {
			autossh, err = opts.LookPath("autossh")
		} else {
			autossh, err = LookPathAutossh()
		}
		if err != nil {
			return fmt.Errorf("install autossh")
		}
	}

	cmd := exec.Command(autossh, AutosshArgs(p.SSHConfig())...)
	cmd.Env = autosshEnv(os.Environ())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start autossh: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go heartbeatLoop(hbCtx, p, opts)

	select {
	case waitErr := <-waitCh:
		hbCancel()
		if waitErr != nil {
			slog.Error("autossh_exit", "err", waitErr)
			return fmt.Errorf("autossh_exit: %w", waitErr)
		}
		slog.Error("autossh_exit")
		return fmt.Errorf("autossh_exit")
	case <-ctx.Done():
		hbCancel()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		<-waitCh
		return nil
	}
}

func heartbeatLoop(ctx context.Context, p Paths, opts RunOptions) {
	interval := heartbeatEvery
	jitter := heartbeatJitter
	if opts.Heartbeat > 0 {
		interval = opts.Heartbeat
		jitter = opts.Jitter
	}
	sshPath := opts.SSH
	if sshPath == "" {
		var err error
		sshPath, err = LookPathSSH()
		if err != nil {
			slog.Warn("heartbeat", "err", err)
			return
		}
	}
	for {
		delay := nextHeartbeat(interval, jitter)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if ctx.Err() != nil {
				return
			}
			if err := sendHeartbeat(ctx, sshPath, p.SSHConfigRPC()); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("heartbeat", "err", err)
			}
		}
	}
}

func sendHeartbeat(ctx context.Context, sshPath, rpcConfig string) error {
	hctx, cancel := context.WithTimeout(ctx, heartbeatWait)
	defer cancel()
	cmd := exec.CommandContext(hctx, sshPath, HeartbeatArgs(rpcConfig)...)
	cmd.Stdin = bytes.NewBufferString(heartbeatBody)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := bytes.TrimSpace(stderr.Bytes())
		if len(msg) > 0 {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func nextHeartbeat(interval, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return interval
	}
	span := int64(jitter)*2 + 1
	n, err := rand.Int(rand.Reader, big.NewInt(span))
	if err != nil {
		return interval
	}
	d := interval - jitter + time.Duration(n.Int64())
	if d < 0 {
		return 0
	}
	return d
}
