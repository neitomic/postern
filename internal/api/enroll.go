package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/neitomic/postern/internal/alloc"
	"github.com/neitomic/postern/internal/auth"
	"github.com/neitomic/postern/internal/names"
	"github.com/neitomic/postern/internal/store"
)

type enrollRequest struct {
	V         int      `json:"v"`
	Token     string   `json:"token"`
	Name      string   `json:"name"`
	LoginUser string   `json:"login_user"`
	Pubkey    string   `json:"pubkey"`
	Tags      []string `json:"tags"`
}

type enrollResponse struct {
	OK             bool   `json:"ok"`
	Name           string `json:"name"`
	Port           int    `json:"port"`
	TunnelUser     string `json:"tunnel_user"`
	VPSHostname    string `json:"vps_hostname"`
	LoginUser      string `json:"login_user"`
	KeyFingerprint string `json:"key_fingerprint"`
}

type txAlloc struct {
	tx *sql.Tx
}

func (t txAlloc) HostByName(name string) (*store.Host, error) {
	return store.HostByNameTx(t.tx, name)
}

func (t txAlloc) ListHosts() ([]*store.Host, error) {
	return store.ListHostsTx(t.tx)
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if req.V != 1 {
		writeError(w, http.StatusBadRequest, "unsupported_version", "unsupported version")
		return
	}

	key, err := auth.ParseEnrollPubkey([]byte(req.Pubkey))
	if err != nil {
		slog.Warn("enroll_denied", "reason", "invalid_pubkey")
		writeError(w, http.StatusBadRequest, "invalid_pubkey", "invalid pubkey")
		return
	}
	stored := auth.MarshalStoredKey(key)
	fp := auth.Fingerprint(key)

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if err := names.Valid(name); err != nil {
		slog.Warn("enroll_denied", "reason", "invalid_name", "name", name)
		writeError(w, http.StatusBadRequest, "invalid_name", err.Error())
		return
	}
	if err := names.ValidLoginUser(req.LoginUser); err != nil {
		slog.Warn("enroll_denied", "reason", "invalid_login_user", "name", name)
		writeError(w, http.StatusBadRequest, "invalid_login_user", err.Error())
		return
	}
	if err := names.ValidTags(req.Tags); err != nil {
		code := "invalid_tag"
		if errors.Is(err, names.ErrTagCount) {
			code = "too_many_tags"
		}
		slog.Warn("enroll_denied", "reason", code, "name", name)
		writeError(w, http.StatusBadRequest, code, err.Error())
		return
	}
	tagsJSON, err := json.Marshal(req.Tags)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to encode tags")
		return
	}
	if req.Tags == nil {
		tagsJSON = []byte("[]")
	}

	id, secret, err := auth.Parse(req.Token)
	if err != nil {
		slog.Warn("enroll_denied", "reason", "invalid_token", "name", name)
		writeError(w, http.StatusBadRequest, "invalid_token", "invalid token")
		return
	}

	probe := s.Probe
	if probe == nil {
		writeError(w, http.StatusInternalServerError, "internal", "listen probe not configured")
		return
	}

	now := time.Now().Unix()
	tx, err := s.Store.BeginImmediate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to begin transaction")
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := store.ConsumeTokenTx(tx, id, auth.Hash(id, secret), name, now); err != nil {
		code, status, msg := tokenConsumeError(err)
		slog.Warn("enroll_denied", "reason", code, "name", name, "token_id", id)
		writeError(w, status, code, msg)
		return
	}

	existing, err := store.HostByNameTx(tx, name)
	if err != nil && !errors.Is(err, store.ErrHostNotFound) {
		writeError(w, http.StatusInternalServerError, "internal", "failed to load host")
		return
	}

	var (
		port     int
		inserted bool
		snapshot *store.Host
		written  *store.Host
	)
	switch {
	case existing == nil:
		h := &store.Host{
			Name:           name,
			LoginUser:      req.LoginUser,
			KeyFingerprint: fp,
			Pubkey:         stored,
			TagsJSON:       string(tagsJSON),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		port, err = s.insertNewHost(tx, probe, h)
		if err != nil {
			s.writeAllocError(w, name, err)
			return
		}
		inserted = true
		written = h
	case existing.KeyFingerprint == fp:
		snapshot = cloneHost(existing)
		if err := store.UpdateHostEnrollTx(tx, name, req.LoginUser, string(tagsJSON), stored, now); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "failed to update host")
			return
		}
		port = existing.Port
		written = cloneHost(existing)
		written.LoginUser = req.LoginUser
		written.TagsJSON = string(tagsJSON)
		written.Pubkey = stored
		written.UpdatedAt = now
	default:
		slog.Warn("enroll_denied", "reason", "name_collision", "name", name)
		writeError(w, http.StatusConflict, "name_collision", "host "+name+" exists with a different key")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to commit")
		return
	}

	s.keysMu.Lock()
	defer s.keysMu.Unlock()
	if _, err := s.renderAuthorizedKeysLocked(); err != nil {
		slog.Error("keys_render_required", "name", name)
		s.compensateRenderFail(inserted, written, snapshot)
		writeError(w, http.StatusInternalServerError, "keys_render_required", "failed to render authorized_keys")
		return
	}

	slog.Info("enroll", "name", name, "port", port, "fingerprint", fp, "token_id", id, "reuse", !inserted)
	writeJSON(w, http.StatusOK, enrollResponse{
		OK:             true,
		Name:           name,
		Port:           port,
		TunnelUser:     s.Config.TunnelUser,
		VPSHostname:    s.Config.VPSHostname,
		LoginUser:      req.LoginUser,
		KeyFingerprint: fp,
	})
}

func (s *Server) insertNewHost(tx *sql.Tx, probe alloc.ListenProbe, h *store.Host) (int, error) {
	min, max := s.Config.PortMin, s.Config.PortMax
	res, err := alloc.Allocate(txAlloc{tx}, probe, h.Name, h.KeyFingerprint, min, max)
	if err != nil {
		return 0, err
	}
	h.Port = res.Port
	err = store.InsertHostTx(tx, h)
	if col, ok := store.UniqueColumn(err); ok && col == "hosts.port" {
		res, err = alloc.Allocate(txAlloc{tx}, probe, h.Name, h.KeyFingerprint, min, max)
		if err != nil {
			return 0, err
		}
		h.Port = res.Port
		err = store.InsertHostTx(tx, h)
	}
	if err != nil {
		return 0, err
	}
	return h.Port, nil
}

func (s *Server) writeAllocError(w http.ResponseWriter, name string, err error) {
	switch {
	case errors.Is(err, alloc.ErrNameCollision):
		slog.Warn("enroll_denied", "reason", "name_collision", "name", name)
		writeError(w, http.StatusConflict, "name_collision", "host "+name+" exists with a different key")
	case errors.Is(err, alloc.ErrPortsExhausted):
		slog.Warn("enroll_denied", "reason", "ports_exhausted", "name", name)
		writeError(w, http.StatusServiceUnavailable, "ports_exhausted", "no free ports in range")
	case errors.Is(err, names.ErrInvalid), errors.Is(err, names.ErrReserved):
		slog.Warn("enroll_denied", "reason", "invalid_name", "name", name)
		writeError(w, http.StatusBadRequest, "invalid_name", err.Error())
	default:
		if col, ok := store.UniqueColumn(err); ok && col == "hosts.key_fingerprint" {
			slog.Warn("enroll_denied", "reason", "fingerprint_collision", "name", name)
			writeError(w, http.StatusConflict, "fingerprint_collision", "key already enrolled under a different name")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to allocate port")
	}
}

func tokenConsumeError(err error) (code string, status int, msg string) {
	switch {
	case errors.Is(err, store.ErrTokenNotFound), errors.Is(err, store.ErrInvalidToken), errors.Is(err, auth.ErrInvalidToken):
		return "invalid_token", http.StatusBadRequest, "invalid token"
	case errors.Is(err, store.ErrTokenExpired):
		return "token_expired", http.StatusBadRequest, "token expired"
	case errors.Is(err, store.ErrTokenUsed):
		return "token_used", http.StatusConflict, "token already used"
	case errors.Is(err, store.ErrBoundName):
		return "bound_name", http.StatusBadRequest, "token bound to a different name"
	default:
		return "internal", http.StatusInternalServerError, "failed to consume token"
	}
}

func (s *Server) compensateRenderFail(inserted bool, written, snapshot *store.Host) {
	if written == nil {
		return
	}
	if inserted {
		err := s.Store.DeleteHostWritten(written.ID, written.KeyFingerprint, written.CreatedAt, written.UpdatedAt)
		if err != nil {
			level := slog.Error
			if errors.Is(err, store.ErrHostChanged) {
				level = slog.Warn
			}
			level("enroll_compensate", "op", "delete", "name", written.Name, "err", err)
		}
		return
	}
	if snapshot == nil {
		return
	}
	err := s.Store.RestoreHostEnroll(written.ID, written.KeyFingerprint, written.UpdatedAt, snapshot.LoginUser, snapshot.TagsJSON, snapshot.Pubkey, snapshot.UpdatedAt)
	if err != nil {
		level := slog.Error
		if errors.Is(err, store.ErrHostChanged) {
			level = slog.Warn
		}
		level("enroll_compensate", "op", "restore", "name", written.Name, "err", err)
	}
}

func cloneHost(h *store.Host) *store.Host {
	c := *h
	if h.LastSeen != nil {
		v := *h.LastSeen
		c.LastSeen = &v
	}
	return &c
}
