package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neitomic/postern/internal/auth"
	"github.com/neitomic/postern/internal/config"
)

func setupMachine(t *testing.T) (Paths, config.Client) {
	t.Helper()
	root := t.TempDir()
	p := Paths{
		ConfigDir: filepath.Join(root, "config"),
		DataDir:   filepath.Join(root, "data"),
	}
	if err := p.Mkdir(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Client{
		Server:       "debian@vps.example.net",
		Name:         "macbook",
		LoginUser:    "neo",
		LocalSSHPort: 22,
		PosterndPath: "/usr/bin/posternd",
	}
	if err := cfg.Save(p.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	if err := GenerateKey(p.IdentityFile(), KeyComment(cfg.Name)); err != nil {
		t.Fatal(err)
	}
	return p, cfg
}

func testToken(t *testing.T) string {
	t.Helper()
	plain, _, err := auth.Issue(time.Minute, "macbook", "")
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func localFP(t *testing.T, p Paths) string {
	t.Helper()
	fp, err := localFingerprint(p.IdentityPub())
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func mustWriteRequest(t *testing.T, p Paths, cfg config.Client) EnrollRequest {
	t.Helper()
	req, _, err := WriteEnrollRequest(p, cfg, testToken(t), false)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func validResp(fp string) map[string]any {
	return map[string]any{
		"ok":              true,
		"name":            "macbook",
		"port":            2223,
		"tunnel_user":     "postern",
		"vps_hostname":    "vps.example.net",
		"login_user":      "neo",
		"key_fingerprint": fp,
	}
}

func marshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWriteEnrollRequestJSONShape(t *testing.T) {
	t.Parallel()
	p, cfg := setupMachine(t)
	token := testToken(t)
	req, raw, err := WriteEnrollRequest(p, cfg, token, false)
	if err != nil {
		t.Fatal(err)
	}
	if req.V != 1 {
		t.Fatalf("v = %d", req.V)
	}
	if req.Token != token {
		t.Fatalf("token = %q", req.Token)
	}
	if req.Name != "macbook" || req.LoginUser != "neo" {
		t.Fatalf("req = %+v", req)
	}
	if !strings.HasPrefix(req.Pubkey, "ssh-ed25519 ") {
		t.Fatalf("pubkey = %q", req.Pubkey)
	}
	if !strings.Contains(req.Pubkey, "postern:macbook") {
		t.Fatalf("pubkey missing comment: %q", req.Pubkey)
	}
	if req.Tags == nil {
		t.Fatal("tags is nil")
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"v", "token", "name", "login_user", "pubkey", "tags"} {
		if _, ok := decoded[k]; !ok {
			t.Fatalf("missing %s in JSON: %s", k, raw)
		}
	}
	if decoded["v"].(float64) != 1 {
		t.Fatalf("v = %v", decoded["v"])
	}
	if decoded["name"] != "macbook" || decoded["login_user"] != "neo" {
		t.Fatalf("decoded = %v", decoded)
	}

	info, err := os.Stat(p.EnrollRequest())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("enroll-request mode = %o, want 0600", perm)
	}
	disk, err := os.ReadFile(p.EnrollRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(disk) != string(raw) {
		t.Fatal("file contents != returned JSON")
	}
}

func TestWriteEnrollRequestRefusesExistingState(t *testing.T) {
	t.Parallel()
	p, cfg := setupMachine(t)
	if err := os.WriteFile(p.StateFile(), []byte(`{"name":"macbook","port":2223}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteEnrollRequest(p, cfg, testToken(t), false); err == nil {
		t.Fatal("expected error without --force")
	}
	if _, err := os.Stat(p.EnrollRequest()); !os.IsNotExist(err) {
		t.Fatalf("enroll-request written without --force: %v", err)
	}
	if _, _, err := WriteEnrollRequest(p, cfg, testToken(t), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.EnrollRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.StateFile()); err != nil {
		t.Fatal("force must not delete existing state.json")
	}
}

func TestApplyResponseSuccess(t *testing.T) {
	t.Parallel()
	p, cfg := setupMachine(t)
	mustWriteRequest(t, p, cfg)
	fp := localFP(t, p)
	st, err := ApplyResponse(p, cfg, marshalJSON(t, validResp(fp)))
	if err != nil {
		t.Fatal(err)
	}
	if st.Name != "macbook" || st.Port != 2223 || st.TunnelUser != "postern" || st.VPSHostname != "vps.example.net" {
		t.Fatalf("state = %+v", st)
	}
	if _, err := time.Parse(time.RFC3339, st.EnrolledAt); err != nil {
		t.Fatalf("enrolled_at = %q: %v", st.EnrolledAt, err)
	}
	info, err := os.Stat(p.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("state.json mode = %o, want 0600", perm)
	}
	if _, err := os.Stat(p.EnrollRequest()); !os.IsNotExist(err) {
		t.Fatal("enroll-request.json should be deleted after success")
	}
	got, err := LoadState(p.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 2223 || got.Name != "macbook" {
		t.Fatalf("loaded = %+v", got)
	}
	tunnel, err := os.ReadFile(p.SSHConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tunnel), "RemoteForward 127.0.0.1:2223 127.0.0.1:22") {
		t.Fatalf("ssh_config = %s", tunnel)
	}
}

func TestApplyResponseFailedBindLeavesFiles(t *testing.T) {
	t.Parallel()
	p, cfg := setupMachine(t)
	mustWriteRequest(t, p, cfg)
	fp := localFP(t, p)
	reqBefore, err := os.ReadFile(p.EnrollRequest())
	if err != nil {
		t.Fatal(err)
	}

	otherFP := "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	cases := []struct {
		name string
		body any
		want string
	}{
		{"ok_missing", map[string]any{"name": "macbook", "port": 2223, "tunnel_user": "postern", "vps_hostname": "vps.example.net", "key_fingerprint": fp}, "ok is not true"},
		{"ok_false", map[string]any{"ok": false, "error": "name_collision", "message": "host macbook exists with a different key"}, "enroll refused"},
		{"ok_string", map[string]any{"ok": "true", "name": "macbook", "port": 2223, "tunnel_user": "postern", "vps_hostname": "vps.example.net", "key_fingerprint": fp}, "ok is not true"},
		{"name_mismatch", map[string]any{"ok": true, "name": "nuc", "port": 2223, "tunnel_user": "postern", "vps_hostname": "vps.example.net", "key_fingerprint": fp}, "name mismatch"},
		{"fingerprint_mismatch", map[string]any{"ok": true, "name": "macbook", "port": 2223, "tunnel_user": "postern", "vps_hostname": "vps.example.net", "key_fingerprint": otherFP}, "key fingerprint mismatch"},
		{"port_22", map[string]any{"ok": true, "name": "macbook", "port": 22, "tunnel_user": "postern", "vps_hostname": "vps.example.net", "key_fingerprint": fp}, "port 22 out of range"},
		{"port_2199", map[string]any{"ok": true, "name": "macbook", "port": 2199, "tunnel_user": "postern", "vps_hostname": "vps.example.net", "key_fingerprint": fp}, "port 2199 out of range"},
		{"port_2300", map[string]any{"ok": true, "name": "macbook", "port": 2300, "tunnel_user": "postern", "vps_hostname": "vps.example.net", "key_fingerprint": fp}, "port 2300 out of range"},
		{"empty_tunnel_user", map[string]any{"ok": true, "name": "macbook", "port": 2223, "tunnel_user": "", "vps_hostname": "vps.example.net", "key_fingerprint": fp}, "tunnel_user is empty"},
		{"empty_vps_hostname", map[string]any{"ok": true, "name": "macbook", "port": 2223, "tunnel_user": "postern", "vps_hostname": "", "key_fingerprint": fp}, "vps_hostname is empty"},
		{"injected_vps_hostname", map[string]any{"ok": true, "name": "macbook", "port": 2223, "tunnel_user": "postern", "vps_hostname": "evil.example\n    StrictHostKeyChecking no\n    UserKnownHostsFile /dev/null", "key_fingerprint": fp}, "invalid vps_hostname"},
		{"injected_tunnel_user", map[string]any{"ok": true, "name": "macbook", "port": 2223, "tunnel_user": "postern\n    StrictHostKeyChecking no", "vps_hostname": "vps.example.net", "key_fingerprint": fp}, "invalid tunnel_user"},
		{"quoted_vps_hostname", map[string]any{"ok": true, "name": "macbook", "port": 2223, "tunnel_user": "postern", "vps_hostname": `evil"example.net`, "key_fingerprint": fp}, "invalid vps_hostname"},
		{"not_json", "not-json", "invalid enroll response JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw []byte
			if s, ok := tc.body.(string); ok {
				raw = []byte(s)
			} else {
				raw = marshalJSON(t, tc.body)
			}
			_, err := ApplyResponse(p, cfg, raw)
			if err == nil {
				t.Fatal("expected bind error")
			}
			var be *BindError
			if !errors.As(err, &be) {
				t.Fatalf("err type %T: %v", err, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %q, want substring %q", err.Error(), tc.want)
			}
			if _, err := os.Stat(p.StateFile()); !os.IsNotExist(err) {
				t.Fatal("state.json must stay absent")
			}
			got, err := os.ReadFile(p.EnrollRequest())
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(reqBefore) {
				t.Fatal("enroll-request.json was modified")
			}
			if _, err := os.Stat(p.SSHConfig()); !os.IsNotExist(err) {
				t.Fatal("ssh_config must stay absent")
			}
			if _, err := os.Stat(p.SSHConfigRPC()); !os.IsNotExist(err) {
				t.Fatal("ssh_config.rpc must stay absent")
			}
		})
	}
}

func TestApplyResponseWriteSSHConfigsFailRemovesState(t *testing.T) {
	t.Parallel()
	p, cfg := setupMachine(t)
	mustWriteRequest(t, p, cfg)
	reqBefore, err := os.ReadFile(p.EnrollRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p.SSHConfig(), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = ApplyResponse(p, cfg, marshalJSON(t, validResp(localFP(t, p))))
	if err == nil {
		t.Fatal("expected WriteSSHConfigs error")
	}
	var be *BindError
	if errors.As(err, &be) {
		t.Fatalf("WriteSSHConfigs failure should not be BindError: %v", err)
	}
	if _, err := os.Stat(p.StateFile()); !os.IsNotExist(err) {
		t.Fatal("state.json must be removed if WriteSSHConfigs fails")
	}
	got, err := os.ReadFile(p.EnrollRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(reqBefore) {
		t.Fatal("enroll-request.json was modified")
	}
}

func TestApplyResponseNameCaseInsensitive(t *testing.T) {
	t.Parallel()
	p, cfg := setupMachine(t)
	mustWriteRequest(t, p, cfg)
	body := validResp(localFP(t, p))
	body["name"] = "MacBook"
	if _, err := ApplyResponse(p, cfg, marshalJSON(t, body)); err != nil {
		t.Fatal(err)
	}
}
