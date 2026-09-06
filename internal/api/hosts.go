package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/neitomic/postern/internal/alloc"
	"github.com/neitomic/postern/internal/auth"
	"github.com/neitomic/postern/internal/names"
	"github.com/neitomic/postern/internal/store"
)

const (
	statusDisabled = "disabled"
	statusOnline   = "online"
	statusDegraded = "degraded"
	statusOffline  = "offline"
)

type hostView struct {
	Name           string   `json:"name"`
	LoginUser      string   `json:"login_user"`
	Port           int      `json:"port"`
	KeyFingerprint string   `json:"key_fingerprint"`
	Tags           []string `json:"tags"`
	LastSeen       *int64   `json:"last_seen"`
	Disabled       bool     `json:"disabled"`
	AgentOnline    bool     `json:"agent_online"`
	TunnelOnline   bool     `json:"tunnel_online"`
	Status         string   `json:"status"`
	Pubkey         string   `json:"pubkey,omitempty"`
}

type renameRequest struct {
	NewName string `json:"new_name"`
}

type rekeyRequest struct {
	OldFingerprint string `json:"old_fingerprint"`
	Pubkey         string `json:"pubkey"`
}

type rmResponse struct {
	OK     bool   `json:"ok"`
	Name   string `json:"name"`
	Port   int    `json:"port"`
	Listen bool   `json:"listen"`
	PID    *int   `json:"pid,omitempty"`
	Killed bool   `json:"killed,omitempty"`
}

type portRow struct {
	Name string `json:"name,omitempty"`
	Port int    `json:"port"`
	PID  *int   `json:"pid,omitempty"`
}

type portsAudit struct {
	DBOwnedNotListening []portRow `json:"db_owned_not_listening"`
	ListeningNotDBOwned []portRow `json:"listening_not_db_owned"`
	Both                []portRow `json:"both"`
}

type gcResponse struct {
	OK            bool       `json:"ok"`
	ExpiredTokens int        `json:"expired_tokens"`
	Ports         portsAudit `json:"ports"`
}

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	list, listening, err := s.loadHostsListening()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to list hosts")
		return
	}
	now := s.now().Unix()
	timeout := s.agentTimeout()
	out := make([]hostView, 0, len(list))
	for _, h := range list {
		out = append(out, hostViewFrom(h, now, timeout, listening, false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleShowHost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h, err := s.Store.HostByName(name)
	if err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to load host")
		return
	}
	listening, err := s.listening()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to probe listen")
		return
	}
	view := hostViewFrom(h, s.now().Unix(), s.agentTimeout(), listening, true)
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	kill := r.URL.Query().Get("kill_listen") == "1" || strings.EqualFold(r.URL.Query().Get("kill_listen"), "true")

	h, err := s.Store.HostByName(name)
	if err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to load host")
		return
	}
	snapshot := cloneHost(h)

	if err := s.Store.DeleteHostByName(name); err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to delete host")
		return
	}

	if !s.renderOrCompensate(name, func() {
		if err := s.Store.InsertHostRestored(snapshot); err != nil {
			slog.Error("host_rm_compensate", "name", name, "err", err)
		}
	}) {
		writeError(w, http.StatusInternalServerError, "keys_render_required", "failed to render authorized_keys")
		return
	}

	slog.Info("host_rm", "name", name, "port", h.Port, "kill_listen", kill)

	listening, err := s.listening()
	still := err != nil
	if err != nil {
		slog.Warn("listen_probe", "name", name, "err", err)
	} else {
		_, still = listening[h.Port]
	}
	pid := s.pidOf(h.Port)

	killed := false
	if kill && still {
		if err := s.killListen(h.Port); err != nil {
			slog.Warn("kill_listen", "name", name, "port", h.Port, "err", err)
		} else {
			killed = true
			if listening, err = s.listening(); err == nil {
				_, still = listening[h.Port]
			}
		}
	}

	writeJSON(w, http.StatusOK, rmResponse{
		OK:     true,
		Name:   h.Name,
		Port:   h.Port,
		Listen: still,
		PID:    pid,
		Killed: killed,
	})
}

func (s *Server) handleDisableHost(w http.ResponseWriter, r *http.Request) {
	s.setHostDisabled(w, r, true)
}

func (s *Server) handleEnableHost(w http.ResponseWriter, r *http.Request) {
	s.setHostDisabled(w, r, false)
}

func (s *Server) setHostDisabled(w http.ResponseWriter, r *http.Request, disabled bool) {
	name := r.PathValue("name")
	h, err := s.Store.HostByName(name)
	if err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to load host")
		return
	}
	snapshot := cloneHost(h)
	now := s.now().Unix()
	if err := s.Store.SetHostDisabled(name, disabled, now); err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to update host")
		return
	}
	if !s.renderOrCompensate(name, func() {
		if err := s.Store.UpdateHostSnapshot(snapshot); err != nil {
			slog.Error("host_disabled_compensate", "name", name, "err", err)
		}
	}) {
		writeError(w, http.StatusInternalServerError, "keys_render_required", "failed to render authorized_keys")
		return
	}
	op := "host_enable"
	if disabled {
		op = "host_disable"
	}
	slog.Info(op, "name", name, "port", h.Port)
	h.Disabled = disabled
	h.UpdatedAt = now
	listening, _ := s.listening()
	writeJSON(w, http.StatusOK, hostViewFrom(h, now, s.agentTimeout(), listening, false))
}

func (s *Server) handleRenameHost(w http.ResponseWriter, r *http.Request) {
	oldName := r.PathValue("name")
	var req renameRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	newName := strings.ToLower(strings.TrimSpace(req.NewName))
	if err := names.Valid(newName); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_name", err.Error())
		return
	}

	h, err := s.Store.HostByName(oldName)
	if err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to load host")
		return
	}
	if strings.EqualFold(h.Name, newName) {
		listening, _ := s.listening()
		writeJSON(w, http.StatusOK, hostViewFrom(h, s.now().Unix(), s.agentTimeout(), listening, false))
		return
	}
	snapshot := cloneHost(h)
	now := s.now().Unix()
	if err := s.Store.RenameHost(h.Name, newName, now); err != nil {
		if col, ok := store.UniqueColumn(err); ok && col == "hosts.name" {
			writeError(w, http.StatusConflict, "name_collision", "host "+newName+" already exists")
			return
		}
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to rename host")
		return
	}
	if !s.renderOrCompensate(newName, func() {
		if err := s.Store.UpdateHostSnapshot(snapshot); err != nil {
			slog.Error("host_rename_compensate", "name", oldName, "err", err)
		}
	}) {
		writeError(w, http.StatusInternalServerError, "keys_render_required", "failed to render authorized_keys")
		return
	}
	slog.Info("host_rename", "old", h.Name, "new", newName, "port", h.Port)
	h.Name = newName
	h.UpdatedAt = now
	listening, _ := s.listening()
	writeJSON(w, http.StatusOK, hostViewFrom(h, now, s.agentTimeout(), listening, false))
}

func (s *Server) handleRekeyHost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req rekeyRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	oldFP := strings.TrimSpace(req.OldFingerprint)
	if oldFP == "" {
		writeError(w, http.StatusBadRequest, "invalid_fingerprint", "old_fingerprint required")
		return
	}

	key, err := auth.ParseEnrollPubkey([]byte(req.Pubkey))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pubkey", "invalid pubkey")
		return
	}
	stored := auth.MarshalStoredKey(key)
	fp := auth.Fingerprint(key)

	h, err := s.Store.HostByName(name)
	if err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to load host")
		return
	}
	if h.KeyFingerprint != oldFP {
		writeError(w, http.StatusBadRequest, "fingerprint_mismatch", "old fingerprint does not match")
		return
	}
	snapshot := cloneHost(h)
	now := s.now().Unix()
	if err := s.Store.RekeyHost(h.Name, oldFP, fp, stored, now); err != nil {
		if col, ok := store.UniqueColumn(err); ok && col == "hosts.key_fingerprint" {
			writeError(w, http.StatusConflict, "fingerprint_collision", "key already enrolled under a different name")
			return
		}
		if errors.Is(err, store.ErrFingerprintMismatch) {
			writeError(w, http.StatusBadRequest, "fingerprint_mismatch", "old fingerprint does not match")
			return
		}
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to rekey host")
		return
	}
	if !s.renderOrCompensate(name, func() {
		if err := s.Store.UpdateHostSnapshot(snapshot); err != nil {
			slog.Error("host_rekey_compensate", "name", name, "err", err)
		}
	}) {
		writeError(w, http.StatusInternalServerError, "keys_render_required", "failed to render authorized_keys")
		return
	}
	slog.Info("host_rekey", "name", name, "port", h.Port, "fingerprint", fp)
	h.KeyFingerprint = fp
	h.Pubkey = stored
	h.UpdatedAt = now
	listening, _ := s.listening()
	writeJSON(w, http.StatusOK, hostViewFrom(h, now, s.agentTimeout(), listening, true))
}

func (s *Server) handleGC(w http.ResponseWriter, r *http.Request) {
	now := s.now().Unix()
	n, err := s.Store.ExpireUnusedTokens(now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to expire tokens")
		return
	}
	audit, err := s.buildPortsAudit()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to audit ports")
		return
	}
	logPortsAudit(audit)
	writeJSON(w, http.StatusOK, gcResponse{OK: true, ExpiredTokens: n, Ports: audit})
}

func (s *Server) handlePortsAudit(w http.ResponseWriter, r *http.Request) {
	audit, err := s.buildPortsAudit()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to audit ports")
		return
	}
	logPortsAudit(audit)
	writeJSON(w, http.StatusOK, audit)
}

func (s *Server) buildPortsAudit() (portsAudit, error) {
	list, err := s.Store.ListHosts()
	if err != nil {
		return portsAudit{}, err
	}
	listening, err := s.listening()
	if err != nil {
		return portsAudit{}, err
	}
	pids := s.portPIDs()
	owned := make(map[int]*store.Host, len(list))
	out := portsAudit{
		DBOwnedNotListening: make([]portRow, 0),
		ListeningNotDBOwned: make([]portRow, 0),
		Both:                make([]portRow, 0),
	}
	for _, h := range list {
		owned[h.Port] = h
		row := portRow{Name: h.Name, Port: h.Port}
		if pid, ok := pids[h.Port]; ok {
			p := pid
			row.PID = &p
		}
		if _, ok := listening[h.Port]; ok {
			out.Both = append(out.Both, row)
		} else {
			row.PID = nil
			out.DBOwnedNotListening = append(out.DBOwnedNotListening, row)
		}
	}
	for port := range listening {
		if _, ok := owned[port]; ok {
			continue
		}
		row := portRow{Port: port}
		if pid, ok := pids[port]; ok {
			p := pid
			row.PID = &p
		}
		out.ListeningNotDBOwned = append(out.ListeningNotDBOwned, row)
	}
	sortPortRows(out.DBOwnedNotListening)
	sortPortRows(out.ListeningNotDBOwned)
	sortPortRows(out.Both)
	return out, nil
}

func sortPortRows(rows []portRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Port != rows[j].Port {
			return rows[i].Port < rows[j].Port
		}
		return rows[i].Name < rows[j].Name
	})
}

func logPortsAudit(a portsAudit) {
	var pids []int
	for _, row := range a.Both {
		if row.PID != nil {
			pids = append(pids, *row.PID)
		}
	}
	for _, row := range a.ListeningNotDBOwned {
		if row.PID != nil {
			pids = append(pids, *row.PID)
		}
	}
	slog.Info("ports_audit",
		"stale_listen", len(a.DBOwnedNotListening),
		"orphan_listen", len(a.ListeningNotDBOwned),
		"pids", pids,
	)
}

func (s *Server) loadHostsListening() ([]*store.Host, map[int]struct{}, error) {
	list, err := s.Store.ListHosts()
	if err != nil {
		return nil, nil, err
	}
	listening, err := s.listening()
	if err != nil {
		return nil, nil, err
	}
	return list, listening, nil
}

func (s *Server) listening() (map[int]struct{}, error) {
	probe := s.Probe
	if probe == nil {
		return nil, errors.New("listen probe not configured")
	}
	m, err := probe.Listening(s.Config.PortMin, s.Config.PortMax)
	if err != nil {
		return nil, err
	}
	if m == nil {
		m = map[int]struct{}{}
	}
	return m, nil
}

func (s *Server) portPIDs() map[int]int {
	if s.PortPIDs != nil {
		return s.PortPIDs(s.Config.PortMin, s.Config.PortMax)
	}
	m, err := alloc.ListenPIDs(s.Config.PortMin, s.Config.PortMax)
	if err != nil || m == nil {
		return map[int]int{}
	}
	return m
}

func (s *Server) pidOf(port int) *int {
	if pid, ok := s.portPIDs()[port]; ok {
		p := pid
		return &p
	}
	return nil
}

func (s *Server) killListen(port int) error {
	if s.KillListen != nil {
		return s.KillListen(port)
	}
	return alloc.KillListenPort(port)
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) agentTimeout() time.Duration {
	d := s.Config.HeartbeatOfflineAfter.Duration()
	if d <= 0 {
		return 90 * time.Second
	}
	return d
}

func (s *Server) renderOrCompensate(name string, compensate func()) bool {
	s.keysMu.Lock()
	defer s.keysMu.Unlock()
	if _, err := s.renderAuthorizedKeysLocked(); err != nil {
		slog.Error("keys_render_required", "name", name)
		if compensate != nil {
			compensate()
		}
		return false
	}
	return true
}

func hostViewFrom(h *store.Host, now int64, timeout time.Duration, listening map[int]struct{}, withPubkey bool) hostView {
	agent, tunnel, status := hostLiveness(h, now, timeout, listening)
	v := hostView{
		Name:           h.Name,
		LoginUser:      h.LoginUser,
		Port:           h.Port,
		KeyFingerprint: h.KeyFingerprint,
		Tags:           parseTags(h.TagsJSON),
		LastSeen:       h.LastSeen,
		Disabled:       h.Disabled,
		AgentOnline:    agent,
		TunnelOnline:   tunnel,
		Status:         status,
	}
	if withPubkey {
		v.Pubkey = reSerializedPubkey(h.Pubkey)
	}
	return v
}

func hostLiveness(h *store.Host, now int64, timeout time.Duration, listening map[int]struct{}) (agent, tunnel bool, status string) {
	if h.Disabled {
		return false, false, statusDisabled
	}
	if h.LastSeen != nil && now-*h.LastSeen <= int64(timeout/time.Second) {
		agent = true
	}
	if listening != nil {
		_, tunnel = listening[h.Port]
	}
	switch {
	case tunnel && agent:
		status = statusOnline
	case tunnel:
		status = statusDegraded
	default:
		status = statusOffline
	}
	return agent, tunnel, status
}

func reSerializedPubkey(stored string) string {
	key, err := auth.ParseEnrollPubkey([]byte(stored))
	if err != nil {
		return stored
	}
	return auth.MarshalStoredKey(key)
}

func parseTags(s string) []string {
	if s == "" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(s), &tags); err != nil || tags == nil {
		return []string{}
	}
	return tags
}
