package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neitomic/postern/internal/auth"
	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/store"
	"github.com/neitomic/postern/internal/version"
)

const (
	testPosternUID = 100
	testPosternGID = 200
)

type emptyProbe struct{}

func (emptyProbe) Listening(min, max int) (map[int]struct{}, error) {
	return map[int]struct{}{}, nil
}

func testServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "postern.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.DefaultPosternd()
	cfg.AuthorizedKeysPath = filepath.Join(dir, "authorized_keys")
	return &Server{
		Store:      st,
		Config:     cfg,
		PosternUID: testPosternUID,
		PosternGID: testPosternGID,
		Probe:      emptyProbe{},
	}
}

func do(t *testing.T, s *Server, peer *auth.Peer, method, path string, body any) *httptest.ResponseRecorder {
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
	if peer != nil {
		req = req.WithContext(auth.WithPeer(req.Context(), *peer))
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}

func adminPeer() *auth.Peer {
	return &auth.Peer{UID: 0}
}

func debianPeer() *auth.Peer {
	return &auth.Peer{UID: 1000, GID: 1000, Groups: []uint32{testPosternGID}}
}

func agentPeer() *auth.Peer {
	return &auth.Peer{UID: testPosternUID, GID: testPosternGID, Groups: []uint32{testPosternGID}}
}

func nobodyPeer() *auth.Peer {
	return &auth.Peer{UID: 1000, GID: testPosternGID}
}

func TestHealthAuthorized(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	for _, p := range []*auth.Peer{adminPeer(), debianPeer(), agentPeer()} {
		rr := do(t, s, p, http.MethodGet, "/v1/health", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("peer uid %d: status %d body %s", p.UID, rr.Code, rr.Body)
		}
		var got map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["ok"] != true || got["version"] != version.Version {
			t.Fatalf("body = %v", got)
		}
	}
}

func TestHealthForbidden(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	for _, p := range []*auth.Peer{nil, nobodyPeer()} {
		rr := do(t, s, p, http.MethodGet, "/v1/health", nil)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("status %d, want 403 body %s", rr.Code, rr.Body)
		}
		assertError(t, rr, "forbidden")
	}
}

func TestTokensCRUD(t *testing.T) {
	t.Parallel()
	s := testServer(t)

	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/tokens", map[string]string{
		"ttl":  "15m",
		"name": "macbook",
		"note": "neo's mbp",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("issue: %d %s", rr.Code, rr.Body)
	}
	var issued issueResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if !issued.OK || !strings.HasPrefix(issued.Token, "psn_join_") {
		t.Fatalf("issued = %+v", issued)
	}
	if issued.BoundName == nil || *issued.BoundName != "macbook" {
		t.Fatalf("bound_name = %v", issued.BoundName)
	}

	rr = do(t, s, debianPeer(), http.MethodGet, "/v1/tokens", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	if strings.Contains(body, "secret") || strings.Contains(body, issued.Token) || strings.Contains(body, "psn_join_") {
		t.Fatalf("list leaked secret: %s", body)
	}
	var list []tokenView
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != issued.ID || list[0].Used {
		t.Fatalf("list = %+v", list)
	}

	rr = do(t, s, adminPeer(), http.MethodDelete, "/v1/tokens/"+issued.ID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rr.Code, rr.Body)
	}

	rr = do(t, s, adminPeer(), http.MethodGet, "/v1/tokens", nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("list after revoke = %+v", list)
	}

	rr = do(t, s, adminPeer(), http.MethodDelete, "/v1/tokens/"+issued.ID, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("second revoke: %d", rr.Code)
	}
}

func TestTokensAgentForbidden(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := do(t, s, agentPeer(), http.MethodGet, "/v1/tokens", nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d", rr.Code)
	}
	rr = do(t, s, agentPeer(), http.MethodPost, "/v1/tokens", map[string]string{})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestHostsEmpty(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := do(t, s, adminPeer(), http.MethodGet, "/v1/hosts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	if strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("body = %q, want []", rr.Body.String())
	}
}

func TestHostsListsInserted(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := &store.Host{
		Name:           "macbook",
		LoginUser:      "neo",
		Port:           2223,
		KeyFingerprint: "SHA256:abcd",
		Pubkey:         "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIaaaa",
		TagsJSON:       `["home"]`,
		CreatedAt:      1,
		UpdatedAt:      1,
	}
	if err := s.Store.InsertHost(h); err != nil {
		t.Fatal(err)
	}
	rr := do(t, s, debianPeer(), http.MethodGet, "/v1/hosts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var list []hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "macbook" || list[0].Port != 2223 {
		t.Fatalf("list = %+v", list)
	}
	if len(list[0].Tags) != 1 || list[0].Tags[0] != "home" {
		t.Fatalf("tags = %v", list[0].Tags)
	}
}

func TestAgentRoutesNotImplemented(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := do(t, s, agentPeer(), http.MethodPost, "/v1/agent/heartbeat", map[string]int{"v": 1})
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("heartbeat: %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "not_implemented")

	rr = do(t, s, agentPeer(), http.MethodGet, "/v1/agent/self", nil)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("self: %d %s", rr.Code, rr.Body)
	}

	rr = do(t, s, adminPeer(), http.MethodPost, "/v1/agent/heartbeat", map[string]int{"v": 1})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("admin heartbeat: %d", rr.Code)
	}
}

func TestListenChmodAndUnlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "api.sock")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != socketMode {
		ln.Close()
		t.Fatalf("mode = %o, want %o", perm, socketMode)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	ln2, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()
}

func TestIssueInvalidTTL(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/tokens", map[string]string{"ttl": "48h"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "invalid_ttl")
}

func assertError(t *testing.T, rr *httptest.ResponseRecorder, code string) {
	t.Helper()
	var eb errorBody
	if err := json.Unmarshal(rr.Body.Bytes(), &eb); err != nil {
		t.Fatalf("decode error: %v body %s", err, rr.Body)
	}
	if eb.OK || eb.Error != code || eb.Message == "" {
		t.Fatalf("error body = %+v, want error %q", eb, code)
	}
}
