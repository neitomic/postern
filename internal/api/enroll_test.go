package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/neitomic/postern/internal/store"
)

const (
	testFP1 = "SHA256:xntGiH1ffcYHTKyHuq1uZHIszeGYiFhWDZd0LkSwLjo"
	testFP2 = "SHA256:3OCNeVZDjzddDvnKNfDUw5IR3JCr8VhwSE1rehlhCSc"
)

func issueJoinToken(t *testing.T, s *Server, name string) string {
	t.Helper()
	body := map[string]string{"ttl": "15m"}
	if name != "" {
		body["name"] = name
	}
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/tokens", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("issue token: %d %s", rr.Code, rr.Body)
	}
	var issued issueResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	return issued.Token
}

func enrollReq(token, name, user, pubkey string, tags []string) map[string]any {
	m := map[string]any{
		"v":          1,
		"token":      token,
		"name":       name,
		"login_user": user,
		"pubkey":     pubkey,
	}
	if tags != nil {
		m["tags"] = tags
	}
	return m
}

func decodeEnroll(t *testing.T, rr interface{ Bytes() []byte }) enrollResponse {
	t.Helper()
	var got enrollResponse
	if err := json.Unmarshal(rr.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func listTokens(t *testing.T, s *Server) []tokenView {
	t.Helper()
	rr := do(t, s, adminPeer(), http.MethodGet, "/v1/tokens", nil)
	var list []tokenView
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func unusedTokenCount(t *testing.T, s *Server) int {
	t.Helper()
	n := 0
	for _, tok := range listTokens(t, s) {
		if !tok.Used {
			n++
		}
	}
	return n
}

func TestEnrollInsertNew(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	tok := issueJoinToken(t, s, "macbook")
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok, "macbook", "neo", testPub1+" postern:ignored", []string{"home"}))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	got := decodeEnroll(t, rr.Body)
	if !got.OK || got.Name != "macbook" || got.Port != 2200 || got.LoginUser != "neo" {
		t.Fatalf("resp = %+v", got)
	}
	if got.TunnelUser != "postern" || got.VPSHostname != "vps.example.net" {
		t.Fatalf("resp = %+v", got)
	}
	if got.KeyFingerprint != testFP1 {
		t.Fatalf("fp = %q", got.KeyFingerprint)
	}

	h, err := s.Store.HostByName("macbook")
	if err != nil {
		t.Fatal(err)
	}
	if h.Port != 2200 || h.Pubkey != testPub1 || h.LoginUser != "neo" {
		t.Fatalf("host = %+v", h)
	}
	if unusedTokenCount(t, s) != 0 {
		t.Fatal("token not marked used")
	}

	raw, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `permitlisten="127.0.0.1:2200"`) || !strings.Contains(body, "postern:macbook") || !strings.Contains(body, testPub1) {
		t.Fatalf("keys = %s", body)
	}
	if strings.Contains(body, "permitopen=") {
		t.Fatal("permitopen= in keys")
	}

	rr = do(t, s, adminPeer(), http.MethodGet, "/v1/hosts", nil)
	var list []hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Port != 2200 || list[0].Name != "macbook" {
		t.Fatalf("list = %+v", list)
	}
}

func TestEnrollRenderFailDeletesInserted(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.RenderKeys = func([]*store.Host) error { return errors.New("boom") }
	tok := issueJoinToken(t, s, "macbook")
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok, "macbook", "neo", testPub1, nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "keys_render_required")
	if _, err := s.Store.HostByName("macbook"); !errors.Is(err, store.ErrHostNotFound) {
		t.Fatalf("host after render fail: %v", err)
	}
	if unusedTokenCount(t, s) != 0 {
		t.Fatal("token should stay used after render fail")
	}
}

func TestEnrollReenrollUpdatesWithoutDuplicating(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	tok := issueJoinToken(t, s, "macbook")
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok, "macbook", "neo", testPub1, []string{"home"}))
	if rr.Code != http.StatusOK {
		t.Fatalf("first: %d %s", rr.Code, rr.Body)
	}
	first := decodeEnroll(t, rr.Body)

	tok2 := issueJoinToken(t, s, "macbook")
	rr = do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok2, "macbook", "debian", testPub1, []string{"lab", "desk"}))
	if rr.Code != http.StatusOK {
		t.Fatalf("re-enroll: %d %s", rr.Code, rr.Body)
	}
	second := decodeEnroll(t, rr.Body)
	if second.Port != first.Port || second.Port != 2200 {
		t.Fatalf("port changed: %d -> %d", first.Port, second.Port)
	}
	if second.LoginUser != "debian" {
		t.Fatalf("login_user = %q", second.LoginUser)
	}

	list, err := s.Store.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1 (no duplicate)", len(list))
	}
	if list[0].LoginUser != "debian" || list[0].TagsJSON != `["lab","desk"]` {
		t.Fatalf("host = %+v", list[0])
	}
	if list[0].Port != first.Port {
		t.Fatalf("port = %d", list[0].Port)
	}
}

func TestEnrollRenderFailAfterReenrollRestoresSnapshot(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	tok := issueJoinToken(t, s, "macbook")
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok, "macbook", "neo", testPub1, []string{"home"}))
	if rr.Code != http.StatusOK {
		t.Fatalf("first: %d %s", rr.Code, rr.Body)
	}
	beforeKeys, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}

	s.RenderKeys = func([]*store.Host) error { return errors.New("boom") }
	tok2 := issueJoinToken(t, s, "macbook")
	rr = do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok2, "macbook", "debian", testPub1, []string{"lab"}))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "keys_render_required")

	h, err := s.Store.HostByName("macbook")
	if err != nil {
		t.Fatal(err)
	}
	if h.LoginUser != "neo" || h.TagsJSON != `["home"]` {
		t.Fatalf("snapshot not restored: %+v", h)
	}
	afterKeys, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterKeys) != string(beforeKeys) {
		t.Fatalf("keys changed on failed re-enroll\n got %s\nwant %s", afterKeys, beforeKeys)
	}
	if !strings.Contains(string(afterKeys), "postern:macbook") {
		t.Fatal("previous authorized_keys line missing")
	}
}

func TestEnrollNameCollision(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	tok := issueJoinToken(t, s, "macbook")
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok, "macbook", "neo", testPub1, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("first: %d %s", rr.Code, rr.Body)
	}
	before, err := s.Store.HostByName("macbook")
	if err != nil {
		t.Fatal(err)
	}

	tok2 := issueJoinToken(t, s, "macbook")
	rr = do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok2, "macbook", "neo", testPub2, nil))
	if rr.Code != http.StatusConflict {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "name_collision")
	if unusedTokenCount(t, s) != 1 {
		t.Fatal("409 should roll back token consume")
	}

	after, err := s.Store.HostByName("macbook")
	if err != nil {
		t.Fatal(err)
	}
	if after.KeyFingerprint != before.KeyFingerprint || after.UpdatedAt != before.UpdatedAt || after.Pubkey != before.Pubkey {
		t.Fatalf("row changed on 409: %+v vs %+v", after, before)
	}
}

func TestEnrollParallelDifferentNames(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	tok1 := issueJoinToken(t, s, "macbook")
	tok2 := issueJoinToken(t, s, "nuc")

	var wg sync.WaitGroup
	type outcome struct {
		code int
		body []byte
	}
	ch := make(chan outcome, 2)
	run := func(token, name, pub string) {
		defer wg.Done()
		rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(token, name, "neo", pub, nil))
		ch <- outcome{rr.Code, append([]byte(nil), rr.Body.Bytes()...)}
	}
	wg.Add(2)
	go run(tok1, "macbook", testPub1)
	go run(tok2, "nuc", testPub2)
	wg.Wait()
	close(ch)

	ports := map[int]string{}
	for o := range ch {
		if o.code != http.StatusOK {
			t.Fatalf("status %d %s", o.code, o.body)
		}
		var got enrollResponse
		if err := json.Unmarshal(o.body, &got); err != nil {
			t.Fatal(err)
		}
		if got.Port < 2200 || got.Port > 2299 {
			t.Fatalf("port %d out of range", got.Port)
		}
		if other, ok := ports[got.Port]; ok {
			t.Fatalf("port %d assigned to %s and %s", got.Port, other, got.Name)
		}
		ports[got.Port] = got.Name
	}
	if len(ports) != 2 {
		t.Fatalf("ports = %v", ports)
	}
	list, err := s.Store.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("hosts = %d", len(list))
	}
	keys, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(keys)
	if !strings.Contains(body, "postern:macbook") || !strings.Contains(body, "postern:nuc") {
		t.Fatalf("keys missing a host:\n%s", body)
	}
	for port := range ports {
		want := fmt.Sprintf(`permitlisten="127.0.0.1:%d"`, port)
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in keys:\n%s", want, body)
		}
	}
}

func TestEnrollMaliciousPubkey(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	tok := issueJoinToken(t, s, "macbook")
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok, "macbook", "neo", `command="id" `+testPub1, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "invalid_pubkey")
	if unusedTokenCount(t, s) != 1 {
		t.Fatal("token consumed on invalid pubkey")
	}
	if _, err := s.Store.HostByName("macbook"); !errors.Is(err, store.ErrHostNotFound) {
		t.Fatalf("host written: %v", err)
	}
}

func TestEnrollAgentForbidden(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := do(t, s, agentPeer(), http.MethodPost, "/v1/enroll", enrollReq("x", "macbook", "neo", testPub1, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestEnrollUnsupportedVersion(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", map[string]any{"v": 2, "token": "x"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "unsupported_version")
}

func TestEnrollBoundNameMismatch(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	tok := issueJoinToken(t, s, "macbook")
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok, "nuc", "neo", testPub1, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "bound_name")
}

func TestAuthorizedKeysRenderEndpoint(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	tok := issueJoinToken(t, s, "macbook")
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/enroll", enrollReq(tok, "macbook", "neo", testPub1, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", rr.Code, rr.Body)
	}
	if err := os.Remove(s.Config.AuthorizedKeysPath); err != nil {
		t.Fatal(err)
	}
	rr = do(t, s, adminPeer(), http.MethodPost, "/v1/authorized-keys/render", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("render: %d %s", rr.Code, rr.Body)
	}
	raw, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), testPub1) || !strings.Contains(string(raw), "postern:macbook") {
		t.Fatalf("keys = %s", raw)
	}
	rr = do(t, s, agentPeer(), http.MethodPost, "/v1/authorized-keys/render", map[string]any{})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("agent render: %d", rr.Code)
	}
}
