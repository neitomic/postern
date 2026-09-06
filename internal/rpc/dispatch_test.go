package rpc

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDispatchCIgnoredHonorOriginalCommand(t *testing.T) {
	t.Parallel()
	var api bool
	code := Env{
		Args:  []string{"posternd-shell", "-c", "/usr/bin/posternd-shell"},
		Orig:  AgentCommand,
		DoAPI: func() int { api = true; return 0 },
		Hold:  func() { t.Fatal("must not hold") },
	}.Main()
	if code != 0 || !api {
		t.Fatalf("code=%d api=%v", code, api)
	}
}

func TestDispatchCArgvNotAllowList(t *testing.T) {
	t.Parallel()
	var held, api bool
	code := Env{
		Args:   []string{"posternd-shell", "-c", AgentCommand},
		Orig:   "",
		Isatty: func() bool { return false },
		Hold:   func() { held = true },
		DoAPI:  func() int { api = true; return 0 },
	}.Main()
	if code != 0 || !held || api {
		t.Fatalf("ForceCommand path must ignore argv after -c; code=%d held=%v api=%v", code, held, api)
	}
}

func TestDispatchAllowList(t *testing.T) {
	t.Parallel()
	var api bool
	code := Env{
		Args:  []string{"posternd-shell", "-c", "ignored"},
		Orig:  " " + AgentCommand + " ",
		DoAPI: func() int { api = true; return 17 },
	}.Main()
	if code != 17 || !api {
		t.Fatalf("code=%d api=%v", code, api)
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	t.Parallel()
	for _, orig := range []string{"cat /etc/passwd", "id", "/bin/sh", "heartbeat"} {
		code := Env{
			Orig:   orig,
			Isatty: func() bool { return false },
			Hold:   func() { t.Fatalf("must not hold for %q", orig) },
			DoAPI:  func() int { t.Fatalf("must not run API for %q", orig); return 0 },
		}.Main()
		if code != 1 {
			t.Fatalf("orig %q: code=%d", orig, code)
		}
	}
}

func TestDispatchTTYReject(t *testing.T) {
	t.Parallel()
	code := Env{
		Orig:   "",
		Isatty: func() bool { return true },
		Hold:   func() { t.Fatal("TTY must not hold") },
		DoAPI:  func() int { t.Fatal("TTY must not run API"); return 0 },
	}.Main()
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestDispatchEmptyNonTTYHolds(t *testing.T) {
	t.Parallel()
	var held bool
	code := Env{
		Orig:   "",
		Isatty: func() bool { return false },
		Hold:   func() { held = true },
	}.Main()
	if code != 0 || !held {
		t.Fatalf("code=%d held=%v", code, held)
	}
}

func TestAgentAPIUnknownOp(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	code := Env{
		Name:   "macbook",
		Stdin:  strings.NewReader(`{"v":1,"op":"enroll","path":"/v1/enroll"}`),
		Stdout: &buf,
		HTTP: func(*http.Request) (*http.Response, error) {
			t.Fatal("unknown op must not hit HTTP")
			return nil, nil
		},
	}.runAgentAPI()
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
	assertRPCError(t, buf.Bytes(), "unknown_op")
}

func TestAgentAPIEmptyName(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	code := Env{
		Name:   "  ",
		Stdin:  strings.NewReader(`{"v":1,"op":"heartbeat"}`),
		Stdout: &buf,
		HTTP: func(*http.Request) (*http.Response, error) {
			t.Fatal("empty POSTERN_NAME must not hit HTTP")
			return nil, nil
		},
	}.runAgentAPI()
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
	assertRPCError(t, buf.Bytes(), "unauthorized")
}

func TestAgentAPIUnsupportedVersion(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	code := Env{
		Name:   "macbook",
		Stdin:  strings.NewReader(`{"v":2,"op":"heartbeat"}`),
		Stdout: &buf,
		HTTP: func(*http.Request) (*http.Response, error) {
			t.Fatal("bad version must not hit HTTP")
			return nil, nil
		},
	}.runAgentAPI()
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
	assertRPCError(t, buf.Bytes(), "unsupported_version")
}

func TestAgentAPIHeartbeatMapsToPOST(t *testing.T) {
	t.Parallel()
	var got *http.Request
	var body []byte
	var buf bytes.Buffer
	code := Env{
		Name:   "macbook",
		Stdin:  strings.NewReader(`{"v":1,"op":"heartbeat","name":"evil","last_seen":1}`),
		Stdout: &buf,
		HTTP: func(req *http.Request) (*http.Response, error) {
			got = req
			body, _ = io.ReadAll(req.Body)
			return jsonHTTP(http.StatusOK, `{"ok":true,"port":2223}`)
		},
	}.runAgentAPI()
	if code != 0 {
		t.Fatalf("code=%d out=%s", code, buf.Bytes())
	}
	if got == nil {
		t.Fatal("no HTTP")
	}
	if got.Method != http.MethodPost || got.URL.Path != "/v1/agent/heartbeat" {
		t.Fatalf("mapped to %s %s", got.Method, got.URL)
	}
	if got.Host != "localhost" {
		t.Fatalf("Host = %q", got.Host)
	}
	if got.Header.Get(headerPosternName) != "macbook" {
		t.Fatalf("X-Postern-Name = %q", got.Header.Get(headerPosternName))
	}
	if string(body) != `{"v":1}` {
		t.Fatalf("body = %s (must not forward name/last_seen/path)", body)
	}
	if !strings.Contains(buf.String(), `"ok":true`) {
		t.Fatalf("stdout = %s", buf.String())
	}
}

func TestAgentAPISelfMapsToGET(t *testing.T) {
	t.Parallel()
	var got *http.Request
	var buf bytes.Buffer
	code := Env{
		Name:   "macbook",
		Stdin:  strings.NewReader(`{"v":1,"op":"self"}`),
		Stdout: &buf,
		HTTP: func(req *http.Request) (*http.Response, error) {
			got = req
			return jsonHTTP(http.StatusOK, `{"name":"macbook","port":2223}`)
		},
	}.runAgentAPI()
	if code != 0 || got == nil {
		t.Fatalf("code=%d", code)
	}
	if got.Method != http.MethodGet || got.URL.Path != "/v1/agent/self" {
		t.Fatalf("mapped to %s %s", got.Method, got.URL)
	}
	if got.Body != nil && got.Body != http.NoBody {
		b, _ := io.ReadAll(got.Body)
		if len(bytes.TrimSpace(b)) != 0 {
			t.Fatalf("GET body = %s", b)
		}
	}
}

func TestAgentAPIHTTPErrorExit1(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	code := Env{
		Name:   "macbook",
		Stdin:  strings.NewReader(`{"v":1,"op":"heartbeat"}`),
		Stdout: &buf,
		HTTP: func(*http.Request) (*http.Response, error) {
			return jsonHTTP(http.StatusForbidden, `{"ok":false,"error":"host_disabled","message":"host is disabled"}`)
		},
	}.runAgentAPI()
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
	assertRPCError(t, buf.Bytes(), "host_disabled")
}

func jsonHTTP(status int, body string) (*http.Response, error) {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func assertRPCError(t *testing.T, raw []byte, code string) {
	t.Helper()
	var e rpcError
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	if e.OK || e.Error != code || e.Message == "" {
		t.Fatalf("error = %+v, want %q", e, code)
	}
}
