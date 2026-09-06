package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestHeartbeatSetsLastSeenFromVPSClock(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	now := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return now }
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	insertHost(t, s, "nuc", 2201, testFP2, testPub2)

	rr := doH(t, s, agentPeer(), http.MethodPost, "/v1/agent/heartbeat",
		map[string]string{headerPosternName: "macbook"},
		map[string]any{"v": 1, "name": "nuc", "last_seen": int64(1), "op": "heartbeat"},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got heartbeatResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Port != 2223 {
		t.Fatalf("resp = %+v", got)
	}

	h, err := s.Store.HostByName("macbook")
	if err != nil {
		t.Fatal(err)
	}
	if h.LastSeen == nil || *h.LastSeen != now.Unix() {
		t.Fatalf("last_seen = %v, want VPS now %d (agent timestamp ignored)", h.LastSeen, now.Unix())
	}
	rr = doH(t, s, agentPeer(), http.MethodPost, "/v1/agent/heartbeat",
		map[string]string{headerPosternName: "macbook"},
		map[string]any{"v": 1},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("second heartbeat same second: %d %s", rr.Code, rr.Body)
	}
	other, err := s.Store.HostByName("nuc")
	if err != nil {
		t.Fatal(err)
	}
	if other.LastSeen != nil {
		t.Fatalf("JSON name must not select host, nuc last_seen = %v", other.LastSeen)
	}
}

func TestHeartbeatDisabled403(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	if err := s.Store.SetHostDisabled(h.Name, true, 2); err != nil {
		t.Fatal(err)
	}
	rr := doH(t, s, agentPeer(), http.MethodPost, "/v1/agent/heartbeat",
		map[string]string{headerPosternName: "macbook"},
		map[string]any{"v": 1},
	)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "host_disabled")
	got, err := s.Store.HostByName("macbook")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSeen != nil {
		t.Fatalf("disabled host last_seen stamped: %v", got.LastSeen)
	}
}

func TestHeartbeatNoSuchHost404(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := doH(t, s, agentPeer(), http.MethodPost, "/v1/agent/heartbeat",
		map[string]string{headerPosternName: "macbook"},
		map[string]any{"v": 1},
	)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "no_such_host")
}

func TestHeartbeatEmptyName401(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	rr := do(t, s, agentPeer(), http.MethodPost, "/v1/agent/heartbeat", map[string]any{"v": 1})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "unauthorized")
	h, err := s.Store.HostByName("macbook")
	if err != nil {
		t.Fatal(err)
	}
	if h.LastSeen != nil {
		t.Fatalf("last_seen set without name: %v", h.LastSeen)
	}
}

func TestHeartbeatUnsupportedVersion(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)
	rr := doH(t, s, agentPeer(), http.MethodPost, "/v1/agent/heartbeat",
		map[string]string{headerPosternName: "macbook"},
		map[string]any{"v": 2},
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "unsupported_version")
}

func TestAgentSelf(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.Probe = mapProbe{2223: {}}
	now := time.Unix(1_000_000, 0)
	s.Now = func() time.Time { return now }
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)

	rr := doH(t, s, agentPeer(), http.MethodGet, "/v1/agent/self",
		map[string]string{headerPosternName: "macbook"}, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	var got hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "macbook" || got.Port != 2223 || got.Pubkey != testPub1 {
		t.Fatalf("self = %+v", got)
	}
	if got.AgentOnline || !got.TunnelOnline || got.Status != statusDegraded {
		t.Fatalf("want tunnel up / agent down / degraded, got %+v", got)
	}

	rr = doH(t, s, agentPeer(), http.MethodPost, "/v1/agent/heartbeat",
		map[string]string{headerPosternName: "macbook"}, map[string]any{"v": 1})
	if rr.Code != http.StatusOK {
		t.Fatalf("heartbeat: %d %s", rr.Code, rr.Body)
	}
	rr = doH(t, s, agentPeer(), http.MethodGet, "/v1/agent/self",
		map[string]string{headerPosternName: "macbook"}, nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.AgentOnline || !got.TunnelOnline || got.Status != statusOnline {
		t.Fatalf("after heartbeat: %+v", got)
	}

	rr = doH(t, s, agentPeer(), http.MethodGet, "/v1/agent/self",
		map[string]string{headerPosternName: "missing"}, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", rr.Code)
	}
	assertError(t, rr, "no_such_host")
}

func TestAgentSelfDisabled403(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	if err := s.Store.SetHostDisabled(insertHost(t, s, "macbook", 2223, testFP1, testPub1).Name, true, 2); err != nil {
		t.Fatal(err)
	}
	rr := doH(t, s, agentPeer(), http.MethodGet, "/v1/agent/self",
		map[string]string{headerPosternName: "macbook"}, nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	assertError(t, rr, "host_disabled")
}

func TestStatusDegradedWhenTunnelUpAgentDown(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	now := time.Unix(5_000, 0)
	s.Now = func() time.Time { return now }
	s.Probe = mapProbe{2223: {}}
	insertHost(t, s, "macbook", 2223, testFP1, testPub1)

	rr := do(t, s, adminPeer(), http.MethodGet, "/v1/hosts/macbook", nil)
	var view hostView
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.TunnelOnline || view.AgentOnline || view.Status != statusDegraded {
		t.Fatalf("before heartbeat: %+v", view)
	}

	rr = doH(t, s, agentPeer(), http.MethodPost, "/v1/agent/heartbeat",
		map[string]string{headerPosternName: "macbook"}, map[string]any{"v": 1})
	if rr.Code != http.StatusOK {
		t.Fatalf("heartbeat: %d %s", rr.Code, rr.Body)
	}
	rr = do(t, s, adminPeer(), http.MethodGet, "/v1/hosts/macbook", nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.AgentOnline || !view.TunnelOnline || view.Status != statusOnline {
		t.Fatalf("after heartbeat: %+v", view)
	}

	s.Now = func() time.Time { return now.Add(91 * time.Second) }
	rr = do(t, s, adminPeer(), http.MethodGet, "/v1/hosts/macbook", nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.AgentOnline || !view.TunnelOnline || view.Status != statusDegraded {
		t.Fatalf("stale last_seen: %+v", view)
	}
}
