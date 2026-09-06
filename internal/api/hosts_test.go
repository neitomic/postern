package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/neitomic/postern/internal/store"
)

type mapProbe map[int]struct{}

func (m mapProbe) Listening(min, max int) (map[int]struct{}, error) {
	out := make(map[int]struct{})
	for p := range m {
		if p >= min && p <= max {
			out[p] = struct{}{}
		}
	}
	return out, nil
}

func insertHost(t *testing.T, s *Server, name string, port int, fp, pub string) *store.Host {
	t.Helper()
	h := &store.Host{
		Name:           name,
		LoginUser:      "neo",
		Port:           port,
		KeyFingerprint: fp,
		Pubkey:         pub,
		TagsJSON:       `["home"]`,
		CreatedAt:      1,
		UpdatedAt:      1,
	}
	if err := s.Store.InsertHost(h); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestListTunnelOnlineFromFakeProbe(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.Probe = mapProbe{2223: {}}
	insertHost(t, s, "macbook", 2223, "SHA256:abcd", testPub1)
	insertHost(t, s, "nuc", 2201, "SHA256:efgh", testPub2)

	rr := do(t, s, adminPeer(), http.MethodGet, "/v1/hosts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var list []hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	byName := map[string]hostView{}
	for _, h := range list {
		byName[h.Name] = h
	}
	mac := byName["macbook"]
	if !mac.TunnelOnline || mac.AgentOnline || mac.Status != statusDegraded || mac.Disabled {
		t.Fatalf("macbook = %+v", mac)
	}
	if mac.Pubkey != "" {
		t.Fatal("list must omit pubkey")
	}
	if mac.LoginUser != "neo" || mac.Port != 2223 || mac.KeyFingerprint != "SHA256:abcd" {
		t.Fatalf("macbook fields = %+v", mac)
	}
	if len(mac.Tags) != 1 || mac.Tags[0] != "home" || mac.LastSeen != nil {
		t.Fatalf("macbook tags/last_seen = %+v", mac)
	}
	nuc := byName["nuc"]
	if nuc.TunnelOnline || nuc.AgentOnline || nuc.Status != statusOffline {
		t.Fatalf("nuc = %+v", nuc)
	}
}

func TestListAgentOnlineFromLastSeen(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	now := time.Unix(1_000_000, 0)
	s.Now = func() time.Time { return now }
	s.Probe = mapProbe{2223: {}}
	h := insertHost(t, s, "macbook", 2223, "SHA256:abcd", testPub1)
	seen := now.Unix() - 30
	h.LastSeen = &seen
	if err := s.Store.UpdateHostSnapshot(h); err != nil {
		t.Fatal(err)
	}

	rr := do(t, s, adminPeer(), http.MethodGet, "/v1/hosts", nil)
	var list []hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].AgentOnline || !list[0].TunnelOnline || list[0].Status != statusOnline {
		t.Fatalf("list = %+v", list)
	}

	stale := now.Unix() - 91
	h.LastSeen = &stale
	if err := s.Store.UpdateHostSnapshot(h); err != nil {
		t.Fatal(err)
	}
	rr = do(t, s, adminPeer(), http.MethodGet, "/v1/hosts", nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list[0].AgentOnline || list[0].Status != statusDegraded {
		t.Fatalf("stale last_seen = %+v", list[0])
	}
}

func TestListDisabledStatus(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.Probe = mapProbe{2223: {}}
	h := insertHost(t, s, "macbook", 2223, "SHA256:abcd", testPub1)
	if err := s.Store.SetHostDisabled(h.Name, true, 2); err != nil {
		t.Fatal(err)
	}
	rr := do(t, s, adminPeer(), http.MethodGet, "/v1/hosts", nil)
	var list []hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != statusDisabled || list[0].TunnelOnline || list[0].AgentOnline || !list[0].Disabled {
		t.Fatalf("list = %+v", list)
	}
}

func TestShowIncludesReserializedPubkey(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	insertHost(t, s, "macbook", 2223, testFP1, testPub1+" leftover-comment")
	rr := do(t, s, adminPeer(), http.MethodGet, "/v1/hosts/macbook", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Pubkey != testPub1 {
		t.Fatalf("pubkey = %q", got.Pubkey)
	}
	if got.Name != "macbook" || got.Status != statusOffline {
		t.Fatalf("got = %+v", got)
	}

	rr = do(t, s, adminPeer(), http.MethodGet, "/v1/hosts/missing", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d", rr.Code)
	}
	assertError(t, rr, "no_such_host")
}

func TestRmWarnsIfStillListen(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.Probe = mapProbe{2223: {}}
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	if err := WriteAuthorizedKeys(s.Config.AuthorizedKeysPath, []*store.Host{
		{Name: "macbook", Port: 2223, Pubkey: testPub1},
	}); err != nil {
		t.Fatal(err)
	}

	rr := do(t, s, adminPeer(), http.MethodDelete, "/v1/hosts/macbook", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got rmResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Name != "macbook" || got.Port != 2223 || !got.Listen || got.Killed {
		t.Fatalf("rm = %+v", got)
	}
	if _, err := s.Store.HostByName("macbook"); !errors.Is(err, store.ErrHostNotFound) {
		t.Fatalf("host still present: %v", err)
	}
	raw, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "postern:macbook") {
		t.Fatalf("keys still contain host: %s", raw)
	}
}

func TestRmKillListen(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	ports := mapProbe{2223: {}}
	s.Probe = ports
	s.PortPIDs = func(min, max int) map[int]int { return map[int]int{2223: 4242} }
	killed := 0
	s.KillListen = func(port int) error {
		killed = port
		delete(ports, port)
		return nil
	}
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)

	rr := do(t, s, adminPeer(), http.MethodDelete, "/v1/hosts/macbook?kill_listen=1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got rmResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if killed != 2223 || !got.Killed || got.Listen {
		t.Fatalf("kill = %d resp %+v", killed, got)
	}
	if got.PID == nil || *got.PID != 4242 {
		t.Fatalf("pid = %v", got.PID)
	}
}

func TestRmKillListenPollsUntilGone(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.ListenGoneTries = 5
	s.ListenGoneSleep = 0
	probe := &seqProbe{liveUntil: 2, port: 2223}
	s.Probe = probe
	s.KillListen = func(port int) error { return nil }
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)

	rr := do(t, s, adminPeer(), http.MethodDelete, "/v1/hosts/macbook?kill_listen=1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got rmResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Killed || got.Listen {
		t.Fatalf("want killed and listen gone after poll, got %+v calls=%d", got, probe.n)
	}
	if probe.n < 3 {
		t.Fatalf("probe calls = %d, want at least pre-kill + 2 polls", probe.n)
	}
}

type seqProbe struct {
	n         int
	liveUntil int
	port      int
}

func (p *seqProbe) Listening(min, max int) (map[int]struct{}, error) {
	p.n++
	if p.n > p.liveUntil {
		return map[int]struct{}{}, nil
	}
	return map[int]struct{}{p.port: {}}, nil
}

func TestRmKillListenKilledWhileStillListen(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.ListenGoneTries = 1
	s.Probe = mapProbe{2223: {}}
	s.KillListen = func(port int) error { return nil }
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)

	rr := do(t, s, adminPeer(), http.MethodDelete, "/v1/hosts/macbook?kill_listen=1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got rmResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Killed || !got.Listen {
		t.Fatalf("want killed+listen, got %+v", got)
	}
}

func TestDisableOmittedFromKeys(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	insertHost(t, s, "nuc", 2201, testFP2, testPub2)

	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/nuc/disable", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Disabled || got.Status != statusDisabled || got.Port != 2201 {
		t.Fatalf("disable = %+v", got)
	}
	raw, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "postern:nuc") {
		t.Fatal("disabled host rendered")
	}
	if !strings.Contains(body, "postern:macbook") {
		t.Fatal("enabled host omitted")
	}
	h, err := s.Store.HostByName("nuc")
	if err != nil {
		t.Fatal(err)
	}
	if !h.Disabled || h.Port != 2201 {
		t.Fatalf("row = %+v", h)
	}

	rr = do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/nuc/enable", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", rr.Code, rr.Body)
	}
	raw, err = os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "postern:nuc") {
		t.Fatal("enable did not restore keys")
	}
}

func TestRekeyKeepsPortAndName(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)

	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/macbook/rekey", map[string]string{
		"old_fingerprint": testFP1,
		"pubkey":          testPub2 + " postern:ignored",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "macbook" || got.Port != 2223 || got.KeyFingerprint != testFP2 || got.Pubkey != testPub2 {
		t.Fatalf("rekey = %+v", got)
	}
	h, err := s.Store.HostByName("macbook")
	if err != nil {
		t.Fatal(err)
	}
	if h.Port != 2223 || h.KeyFingerprint != testFP2 || h.Pubkey != testPub2 {
		t.Fatalf("row = %+v", h)
	}
	raw, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, testPub2) || strings.Contains(body, testPub1) {
		t.Fatalf("keys = %s", body)
	}

	rr = do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/macbook/rekey", map[string]string{
		"old_fingerprint": testFP1,
		"pubkey":          testPub1,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("mismatch status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "fingerprint_mismatch")
}

func TestRekeyFingerprintCollision(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	insertHost(t, s, "nuc", 2201, testFP2, testPub2)
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/macbook/rekey", map[string]string{
		"old_fingerprint": testFP1,
		"pubkey":          testPub2,
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "fingerprint_collision")
}

func TestRenameUpdatesKeys(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)

	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/macbook/rename", map[string]string{
		"new_name": "mbp",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "mbp" || got.Port != 2223 {
		t.Fatalf("rename = %+v", got)
	}
	if _, err := s.Store.HostByName("macbook"); !errors.Is(err, store.ErrHostNotFound) {
		t.Fatal("old name still present")
	}
	h, err := s.Store.HostByName("mbp")
	if err != nil {
		t.Fatal(err)
	}
	if h.Port != 2223 {
		t.Fatalf("port changed: %d", h.Port)
	}
	raw, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "POSTERN_NAME=mbp") || !strings.Contains(body, "postern:mbp") {
		t.Fatalf("keys missing new name: %s", body)
	}
	if strings.Contains(body, "postern:macbook") {
		t.Fatal("keys still have old name")
	}

	rr = do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/mbp/rename", map[string]string{
		"new_name": "postern",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("reserved: %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "invalid_name")
}

func TestRenameCollision(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	insertHost(t, s, "nuc", 2201, testFP2, testPub2)
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/macbook/rename", map[string]string{
		"new_name": "nuc",
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "name_collision")
}

func TestPortsAuditCategories(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.Probe = mapProbe{2223: {}, 2205: {}}
	s.PortPIDs = func(min, max int) map[int]int { return map[int]int{2223: 11, 2205: 22} }
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	insertHost(t, s, "nuc", 2201, testFP2, testPub2)

	rr := do(t, s, adminPeer(), http.MethodGet, "/v1/ports", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got portsAudit
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Both) != 1 || got.Both[0].Name != "macbook" || got.Both[0].Port != 2223 || got.Both[0].PID == nil || *got.Both[0].PID != 11 {
		t.Fatalf("both = %+v", got.Both)
	}
	if len(got.DBOwnedNotListening) != 1 || got.DBOwnedNotListening[0].Name != "nuc" || got.DBOwnedNotListening[0].Port != 2201 {
		t.Fatalf("missing listen = %+v", got.DBOwnedNotListening)
	}
	if len(got.ListeningNotDBOwned) != 1 || got.ListeningNotDBOwned[0].Port != 2205 || got.ListeningNotDBOwned[0].PID == nil || *got.ListeningNotDBOwned[0].PID != 22 {
		t.Fatalf("orphan = %+v", got.ListeningNotDBOwned)
	}
}

func TestGCExpiresUnusedTokensAndAudits(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	now := time.Unix(1000, 0)
	s.Now = func() time.Time { return now }
	s.Probe = mapProbe{}
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)

	expired := &store.Token{ID: "aaaaaaaaaaaaaaaa", Kind: store.KindJoin, SecretHash: strings.Repeat("ab", 32), ExpiresAt: 500, CreatedAt: 1}
	valid := &store.Token{ID: "bbbbbbbbbbbbbbbb", Kind: store.KindJoin, SecretHash: strings.Repeat("cd", 32), ExpiresAt: 2000, CreatedAt: 1}
	usedAt := int64(10)
	used := &store.Token{ID: "cccccccccccccccc", Kind: store.KindJoin, SecretHash: strings.Repeat("ef", 32), ExpiresAt: 500, CreatedAt: 1, UsedAt: &usedAt}
	for _, tok := range []*store.Token{expired, valid, used} {
		if err := s.Store.InsertToken(tok); err != nil {
			t.Fatal(err)
		}
	}

	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/gc", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got gcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.ExpiredTokens != 1 {
		t.Fatalf("gc = %+v", got)
	}
	if len(got.Ports.DBOwnedNotListening) != 1 || got.Ports.DBOwnedNotListening[0].Name != "macbook" {
		t.Fatalf("audit = %+v", got.Ports)
	}
	if _, err := s.Store.TokenByID(expired.ID); err == nil {
		t.Fatal("expired unused token still present")
	}
	if _, err := s.Store.TokenByID(valid.ID); err != nil {
		t.Fatalf("valid token removed: %v", err)
	}
	if _, err := s.Store.TokenByID(used.ID); err != nil {
		t.Fatalf("used token removed: %v", err)
	}
	if _, err := s.Store.HostByName("macbook"); err != nil {
		t.Fatalf("gc deleted host: %v", err)
	}
}

func TestHostsAgentForbidden(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := do(t, s, agentPeer(), http.MethodGet, "/v1/hosts", nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d", rr.Code)
	}
	rr = do(t, s, agentPeer(), http.MethodPost, "/v1/gc", map[string]any{})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("gc status %d", rr.Code)
	}
}

func TestRmRenderFailRestoresRow(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	before := insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	if err := WriteAuthorizedKeys(s.Config.AuthorizedKeysPath, []*store.Host{before}); err != nil {
		t.Fatal(err)
	}
	keysBefore, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	s.RenderKeys = func([]*store.Host) error { return errors.New("boom") }

	rr := do(t, s, adminPeer(), http.MethodDelete, "/v1/hosts/macbook", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "keys_render_required")
	assertHostUnchanged(t, s, before)
	keysAfter, err := os.ReadFile(s.Config.AuthorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(keysAfter) != string(keysBefore) {
		t.Fatal("keys changed on failed rm")
	}
}

func TestDisableRenderFailRestoresRow(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	before := insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	s.RenderKeys = func([]*store.Host) error { return errors.New("boom") }
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/macbook/disable", map[string]any{})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "keys_render_required")
	assertHostUnchanged(t, s, before)
}

func TestRekeyRenderFailRestoresRow(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	before := insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	s.RenderKeys = func([]*store.Host) error { return errors.New("boom") }
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/macbook/rekey", map[string]string{
		"old_fingerprint": testFP1,
		"pubkey":          testPub2,
	})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "keys_render_required")
	assertHostUnchanged(t, s, before)
}

func TestRenameRenderFailRestoresRow(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	before := insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	s.RenderKeys = func([]*store.Host) error { return errors.New("boom") }
	rr := do(t, s, adminPeer(), http.MethodPost, "/v1/hosts/macbook/rename", map[string]string{
		"new_name": "mbp",
	})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "keys_render_required")
	assertHostUnchanged(t, s, before)
	if _, err := s.Store.HostByName("mbp"); !errors.Is(err, store.ErrHostNotFound) {
		t.Fatal("new name present after failed rename")
	}
}

func assertHostUnchanged(t *testing.T, s *Server, want *store.Host) {
	t.Helper()
	got, err := s.Store.HostByName(want.Name)
	if err != nil {
		t.Fatalf("HostByName(%s): %v", want.Name, err)
	}
	if got.Name != want.Name || got.Port != want.Port || got.KeyFingerprint != want.KeyFingerprint || got.Disabled != want.Disabled || got.Pubkey != want.Pubkey || got.LoginUser != want.LoginUser {
		t.Fatalf("row changed:\n got %+v\nwant %+v", got, want)
	}
}

func TestHostLivenessFormula(t *testing.T) {
	t.Parallel()
	now := int64(1000)
	timeout := 90 * time.Second
	listening := map[int]struct{}{2223: {}}
	h := &store.Host{Port: 2223}
	agent, tunnel, status := hostLiveness(h, now, timeout, listening)
	if agent || !tunnel || status != statusDegraded {
		t.Fatalf("no last_seen: %v %v %s", agent, tunnel, status)
	}
	h.Disabled = true
	agent, tunnel, status = hostLiveness(h, now, timeout, listening)
	if agent || tunnel || status != statusDisabled {
		t.Fatalf("disabled: %v %v %s", agent, tunnel, status)
	}
}
