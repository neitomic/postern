package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/neitomic/postern/internal/config"
)

func (e Env) Main() int {
	// ForceCommand path: sshd runs login_shell -c <ForceCommand> and puts the
	// client command in SSH_ORIGINAL_COMMAND. Do not honor argv after -c.
	orig := strings.TrimSpace(e.Orig)
	switch orig {
	case AgentCommand:
		if e.DoAPI != nil {
			return e.DoAPI()
		}
		return e.runAgentAPI()
	case "":
		if e.isTTY() {
			return 1
		}
		e.hold()
		return 0
	default:
		return 1
	}
}

func (e Env) isTTY() bool {
	if e.Isatty != nil {
		return e.Isatty()
	}
	f, ok := e.Stdin.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func (e Env) hold() {
	if e.Hold != nil {
		e.Hold()
		return
	}
	holdUntilSIGTERM()
}

func holdUntilSIGTERM() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(ch)
	<-ch
}

func (e Env) runAgentAPI() int {
	out := e.Stdout
	if out == nil {
		out = io.Discard
	}
	name := strings.TrimSpace(e.Name)
	if name == "" {
		writeRPCError(out, "unauthorized", "POSTERN_NAME required")
		return 1
	}

	in := e.Stdin
	if in == nil {
		in = bytes.NewReader(nil)
	}
	var req rpcRequest
	dec := json.NewDecoder(io.LimitReader(in, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeRPCError(out, "invalid_json", "invalid JSON body")
		return 1
	}
	if req.V != 1 {
		writeRPCError(out, "unsupported_version", "unsupported version")
		return 1
	}

	var method, path string
	var body []byte
	switch req.Op {
	case "heartbeat":
		method, path = http.MethodPost, "/v1/agent/heartbeat"
		body = []byte(`{"v":1}`)
	case "self":
		method, path = http.MethodGet, "/v1/agent/self"
	default:
		writeRPCError(out, "unknown_op", "unknown op")
		return 1
	}

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequest(method, "http://localhost"+path, rdr)
	if err != nil {
		writeRPCError(out, "internal", "failed to build request")
		return 1
	}
	httpReq.Host = "localhost"
	httpReq.Header.Set(headerPosternName, name)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.httpDo(httpReq)
	if err != nil {
		writeRPCError(out, "internal", "api request failed")
		return 1
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		writeRPCError(out, "internal", "failed to read response")
		return 1
	}
	if len(raw) > 0 {
		_, _ = out.Write(raw)
		if raw[len(raw)-1] != '\n' {
			_, _ = out.Write([]byte("\n"))
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0
	}
	return 1
}

func (e Env) httpDo(req *http.Request) (*http.Response, error) {
	if e.HTTP != nil {
		return e.HTTP(req)
	}
	sock := e.Socket
	if sock == "" {
		sock = config.DefaultPosternd().SocketPath
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}
	return client.Do(req)
}

func writeRPCError(w io.Writer, code, msg string) {
	_ = json.NewEncoder(w).Encode(rpcError{OK: false, Error: code, Message: msg})
}
