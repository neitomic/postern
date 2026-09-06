//go:build integration

package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/neitomic/postern/internal/api"
	"github.com/neitomic/postern/internal/auth"
	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/store"
)

// TestSSHDMergeGate is the PR 6 required merge gate.
// Skip the whole test only if sshd is absent. CI must install openssh-server.
//
// Path proven: ForceCommand + SSH_ORIGINAL_COMMAND (contrib/sshd/50-postern.conf).
// sshd execs login_shell -c ForceCommand; the dispatcher ignores argv after -c.
// ssh -N does not run ForceCommand; reverse forwards still LISTEN.
func TestSSHDMergeGate(t *testing.T) {
	sshd := findSSHD(t)
	root := repoRoot(t)
	fixture, err := os.ReadFile(filepath.Join(root, "contrib", "sshd", "50-postern.conf"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("config", func(t *testing.T) {
		testSSHDConfig(t, sshd, string(fixture))
	})
	t.Run("live", func(t *testing.T) {
		testSSHDLive(t, sshd, root, string(fixture))
	})
}

func testSSHDConfig(t *testing.T, sshd, fixture string) {
	t.Helper()
	dir := t.TempDir()
	hostKey := filepath.Join(dir, "host_ed25519")
	run(t, exec.Command("ssh-keygen", "-t", "ed25519", "-f", hostKey, "-N", "", "-q"))

	cfgPath := filepath.Join(dir, "sshd_config")
	var b strings.Builder
	fmt.Fprintf(&b, "Port 2222\nListenAddress 127.0.0.1\nHostKey %s\nPidFile %s\nStrictModes no\n", hostKey, filepath.Join(dir, "sshd.pid"))
	b.WriteString(fixture)
	if !strings.HasSuffix(strings.TrimSpace(fixture), "Match all") {
		t.Fatal("fixture must end with Match all")
	}
	// Keywords illegal inside Match: proves Match all closed the postern block.
	fmt.Fprintf(&b, "\nPort 2222\nListenAddress 127.0.0.1\n")
	if err := os.WriteFile(cfgPath, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(sshd, "-t", "-f", cfgPath, "-e")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sshd -t: %v\n%s", err, out)
	}

	postern := sshdT(t, sshd, cfgPath, "postern")
	if got := postern["forcecommand"]; got != "/usr/bin/posternd-shell" {
		t.Fatalf("postern forcecommand = %q", got)
	}
	if got := postern["permitopen"]; got != "none" {
		t.Fatalf("postern permitopen = %q", got)
	}
	if got := postern["allowtcpforwarding"]; got != "remote" {
		t.Fatalf("postern allowtcpforwarding = %q", got)
	}

	debian := sshdT(t, sshd, cfgPath, "debian")
	if got := debian["forcecommand"]; got == "/usr/bin/posternd-shell" {
		t.Fatalf("debian leaked ForceCommand: %q", got)
	}
	if got := debian["permitty"]; strings.EqualFold(got, "no") {
		t.Fatalf("debian leaked PermitTTY no")
	}
}

func testSSHDLive(t *testing.T, sshd, root, fixture string) {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	sock := shortSock(t)
	t.Cleanup(func() { _ = os.Remove(sock) })

	dbPath := filepath.Join(dir, "postern.db")
	keysPath := filepath.Join(dir, "authorized_keys")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := config.DefaultPosternd()
	cfg.AuthorizedKeysPath = keysPath
	cfg.SocketPath = sock
	uid := uint32(os.Getuid())
	srv := &api.Server{
		Store:      st,
		Config:     cfg,
		PosternUID: uid,
		PosternGID: uint32(os.Getgid()),
		Probe:      bindProbe{},
		LookupPeer: func(net.Conn) (auth.Peer, error) {
			return auth.Peer{UID: uid}, nil
		},
	}

	rejectMaliciousEnroll(t, srv, root)

	ln, err := api.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	httpSrv := srv.HTTPServer()
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	})

	idKey := filepath.Join(dir, "id_ed25519")
	run(t, exec.Command("ssh-keygen", "-t", "ed25519", "-f", idKey, "-N", "", "-q", "-C", "postern:macbook"))
	pubRaw, err := os.ReadFile(idKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}

	tok := issueToken(t, srv, "macbook")
	enroll := adminJSON(t, srv, http.MethodPost, "/v1/enroll", map[string]any{
		"v": 1, "token": tok, "name": "macbook", "login_user": "neo",
		"pubkey": strings.TrimSpace(string(pubRaw)),
	})
	if enroll.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", enroll.Code, enroll.Body)
	}
	var enrolled struct {
		OK   bool `json:"ok"`
		Port int  `json:"port"`
	}
	if err := json.Unmarshal(enroll.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	if !enrolled.OK || enrolled.Port < cfg.PortMin {
		t.Fatalf("enroll resp %+v", enrolled)
	}
	port := enrolled.Port
	if _, err := os.Stat(keysPath); err != nil {
		t.Fatal(err)
	}

	shell := buildPosterndShell(t, dir, root)
	hostKey := filepath.Join(dir, "host_ed25519")
	run(t, exec.Command("ssh-keygen", "-t", "ed25519", "-f", hostKey, "-N", "", "-q"))

	sshdPort := freePort(t)
	sshdCfg := filepath.Join(dir, "sshd_live.conf")
	if err := os.WriteFile(sshdCfg, []byte(liveSSHDConfig(u.Username, hostKey, filepath.Join(dir, "sshd.pid"), keysPath, shell, sock, sshdPort)), 0o600); err != nil {
		t.Fatal(err)
	}
	tcmd := exec.Command(sshd, "-t", "-f", sshdCfg, "-e")
	if out, err := tcmd.CombinedOutput(); err != nil {
		if runtime.GOOS != "linux" {
			t.Skipf("live sshd_config rejected on %s (%s): %v\n%s", runtime.GOOS, sshd, err, out)
		}
		t.Fatalf("sshd -t live: %v\n%s", err, out)
	}

	sshdCmd, sshdLog := startSSHD(t, sshd, sshdCfg)
	if err := waitTCP(fmt.Sprintf("127.0.0.1:%d", sshdPort), 5*time.Second); err != nil {
		_ = sshdCmd.Process.Kill()
		if runtime.GOOS != "linux" {
			t.Skipf("unprivileged sshd did not listen on %s: %v\n%s", runtime.GOOS, err, sshdLog.String())
		}
		t.Fatalf("sshd did not listen: %v\n%s", err, sshdLog.String())
	}

	ssh := sshClient(t, idKey, sshdPort)

	localLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = localLn.Close() })
	localPort := localLn.Addr().(*net.TCPAddr).Port
	go acceptOnce(localLn)

	tunnel := ssh.cmd("-N", "-o", "ExitOnForwardFailure=yes",
		"-R", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", port, localPort))
	var tunnelLog bytes.Buffer
	tunnel.Stdout = &tunnelLog
	tunnel.Stderr = &tunnelLog
	if err := tunnel.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tunnel.Process.Kill(); _, _ = tunnel.Process.Wait() })
	if err := waitTCP(fmt.Sprintf("127.0.0.1:%d", port), 8*time.Second); err != nil {
		t.Fatalf("ssh -N RemoteForward did not LISTEN on 127.0.0.1:%d: %v\n%s\nsshd:\n%s", port, err, tunnelLog.String(), sshdLog.String())
	}

	hb := ssh.cmd("-T", "postern-agent-api")
	hb.Stdin = strings.NewReader(`{"v":1,"op":"heartbeat"}`)
	var hbErr bytes.Buffer
	hb.Stderr = &hbErr
	out, err := hb.Output()
	if err != nil {
		t.Fatalf("concurrent heartbeat: %v\nstdout=%s\nstderr=%s\nsshd:\n%s", err, out, hbErr.String(), sshdLog.String())
	}
	var hbResp struct {
		OK   bool `json:"ok"`
		Port int  `json:"port"`
	}
	if err := json.Unmarshal(out, &hbResp); err != nil {
		t.Fatalf("heartbeat JSON %q: %v (stderr %s)", out, err, hbErr.String())
	}
	if !hbResp.OK || hbResp.Port != port {
		t.Fatalf("heartbeat = %+v body %s", hbResp, out)
	}

	hold := ssh.cmd("-T")
	var holdLog bytes.Buffer
	hold.Stdout = &holdLog
	hold.Stderr = &holdLog
	if err := hold.Start(); err != nil {
		t.Fatal(err)
	}
	holdDone := make(chan error, 1)
	go func() { holdDone <- hold.Wait() }()
	select {
	case err := <-holdDone:
		t.Fatalf("empty session exited immediately (shell should hold): %v %s", err, holdLog.String())
	case <-time.After(400 * time.Millisecond):
	}
	_ = hold.Process.Kill()
	<-holdDone

	cat := ssh.cmd("cat", "/etc/passwd")
	catOut, err := cat.CombinedOutput()
	if err == nil {
		t.Fatalf("cat /etc/passwd succeeded: %s", catOut)
	}
	if strings.Contains(string(catOut), "root:") {
		t.Fatalf("passwd leaked: %s", catOut)
	}

	tty := ssh.cmd("-t", "cat", "/etc/passwd")
	tty.Stdin = bytes.NewReader(nil)
	ttyOut, err := tty.CombinedOutput()
	if err == nil {
		t.Fatalf("TTY cat succeeded: %s", ttyOut)
	}
	if strings.Contains(string(ttyOut), "root:") {
		t.Fatalf("TTY passwd leaked: %s", ttyOut)
	}
}

func rejectMaliciousEnroll(t *testing.T, srv *api.Server, root string) {
	t.Helper()
	tok := issueToken(t, srv, "evil")
	dir := filepath.Join(root, "testdata", "pubkeys", "malicious")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) == 0 {
		t.Fatal("no malicious pubkeys")
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		rr := adminJSON(t, srv, http.MethodPost, "/v1/enroll", map[string]any{
			"v": 1, "token": tok, "name": "evil", "login_user": "neo", "pubkey": string(raw),
		})
		if rr.Code == http.StatusOK {
			t.Fatalf("malicious %s enrolled: %s", e.Name(), rr.Body)
		}
	}
}

func liveSSHDConfig(user, hostKey, pidFile, keys, forceCmd, sock string, port int) string {
	return fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s
PidFile %s
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
AuthorizedKeysFile %s
PermitUserEnvironment POSTERN_NAME,POSTERN_ROLE
Match User %s
    AllowTcpForwarding remote
    GatewayPorts no
    PermitOpen none
    X11Forwarding no
    AllowAgentForwarding no
    PermitTTY no
    PermitTunnel no
    AllowStreamLocalForwarding no
    AuthorizedKeysFile %s
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    PubkeyAuthentication yes
    SetEnv POSTERND_SOCKET=%s
    ForceCommand %s
Match all
`, port, hostKey, pidFile, keys, user, keys, sock, forceCmd)
}

type sshHelper struct {
	idKey string
	port  int
	user  string
}

func sshClient(t *testing.T, idKey string, port int) sshHelper {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	return sshHelper{idKey: idKey, port: port, user: u.Username}
}

func (s sshHelper) cmd(extra ...string) *exec.Cmd {
	flags := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityFile=" + s.idKey,
		"-o", "PreferredAuthentications=publickey",
		"-o", "PasswordAuthentication=no",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5",
		"-p", fmt.Sprintf("%d", s.port),
	}
	var sshFlags, remote []string
	for i := 0; i < len(extra); i++ {
		a := extra[i]
		switch {
		case a == "-N" || a == "-T" || a == "-t" || a == "-n":
			sshFlags = append(sshFlags, a)
		case a == "-o":
			sshFlags = append(sshFlags, a)
			if i+1 < len(extra) {
				i++
				sshFlags = append(sshFlags, extra[i])
			}
		case a == "-R":
			sshFlags = append(sshFlags, a)
			if i+1 < len(extra) {
				i++
				sshFlags = append(sshFlags, extra[i])
			}
		default:
			remote = append(remote, extra[i:]...)
			i = len(extra)
		}
	}
	args := append(flags, sshFlags...)
	args = append(args, s.user+"@127.0.0.1")
	args = append(args, remote...)
	return exec.Command("ssh", args...)
}

func startSSHD(t *testing.T, sshd, cfg string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	logBuf := &bytes.Buffer{}
	cmd := exec.Command(sshd, "-f", cfg, "-D", "-e")
	cmd.Stdout = logBuf
	cmd.Stderr = logBuf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sshd: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			done := make(chan struct{})
			go func() { _, _ = cmd.Process.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}
	})
	return cmd, logBuf
}

func sshdT(t *testing.T, sshd, cfg, user string) map[string]string {
	t.Helper()
	cmd := exec.Command(sshd, "-T", "-f", cfg, "-C", fmt.Sprintf("user=%s,host=localhost,addr=127.0.0.1", user))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sshd -T user=%s: %v\n%s", user, err, out)
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		m[strings.ToLower(k)] = v
	}
	return m
}

func findSSHD(t *testing.T) string {
	t.Helper()
	for _, p := range []string{"/usr/sbin/sshd", "/usr/bin/sshd"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	p, err := exec.LookPath("sshd")
	if err != nil {
		t.Skip("sshd binary absent")
	}
	return p
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func buildPosterndShell(t *testing.T, dir, root string) string {
	t.Helper()
	bin := filepath.Join(dir, "posternd")
	shell := filepath.Join(dir, "posternd-shell")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/posternd")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build posternd: %v\n%s", err, out)
	}
	if err := os.Symlink("posternd", shell); err != nil {
		t.Fatal(err)
	}
	return shell
}

func shortSock(t *testing.T) string {
	t.Helper()
	p := filepath.Join(os.TempDir(), fmt.Sprintf("psn-%d-%d.sock", os.Getpid(), time.Now().UnixNano()%1e9))
	if len(p) > 90 {
		p = filepath.Join("/tmp", fmt.Sprintf("psn-%d.sock", os.Getpid()))
	}
	return p
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitTCP(addr string, d time.Duration) error {
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		last = err
		time.Sleep(40 * time.Millisecond)
	}
	return last
}

func acceptOnce(ln net.Listener) {
	c, err := ln.Accept()
	if err != nil {
		return
	}
	defer c.Close()
	_, _ = io.Copy(io.Discard, c)
}

func run(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", cmd.String(), err, out)
	}
}

func issueToken(t *testing.T, srv *api.Server, name string) string {
	t.Helper()
	rr := adminJSON(t, srv, http.MethodPost, "/v1/tokens", map[string]string{"ttl": "15m", "name": name})
	if rr.Code != http.StatusOK {
		t.Fatalf("issue token: %d %s", rr.Code, rr.Body)
	}
	var issued struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	return issued.Token
}

func adminJSON(t *testing.T, srv *api.Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, "http://localhost"+path, rdr)
	req.Host = "localhost"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(auth.WithPeer(req.Context(), auth.Peer{UID: 0}))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

type bindProbe struct{}

func (bindProbe) Listening(min, max int) (map[int]struct{}, error) {
	out := map[int]struct{}{}
	for p := min; p <= max; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			out[p] = struct{}{}
			continue
		}
		_ = ln.Close()
	}
	return out, nil
}
