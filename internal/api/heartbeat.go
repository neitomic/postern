package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/neitomic/postern/internal/store"
)

const headerPosternName = "X-Postern-Name"

type heartbeatRequest struct {
	V int `json:"v"`
}

type heartbeatResponse struct {
	OK   bool `json:"ok"`
	Port int  `json:"port"`
}

func agentName(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(headerPosternName))
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	name := agentName(r)
	if name == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "POSTERN_NAME required")
		return
	}

	var req heartbeatRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if req.V != 1 {
		writeError(w, http.StatusBadRequest, "unsupported_version", "unsupported version")
		return
	}

	h, err := s.Store.HostByName(name)
	if err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to load host")
		return
	}
	if h.Disabled {
		writeError(w, http.StatusForbidden, "host_disabled", "host is disabled")
		return
	}

	now := s.now().Unix()
	if err := s.Store.SetLastSeen(name, now); err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to update last_seen")
		return
	}
	slog.Debug("heartbeat", "name", name, "port", h.Port)
	writeJSON(w, http.StatusOK, heartbeatResponse{OK: true, Port: h.Port})
}

func (s *Server) handleAgentSelf(w http.ResponseWriter, r *http.Request) {
	name := agentName(r)
	if name == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "POSTERN_NAME required")
		return
	}
	h, err := s.Store.HostByName(name)
	if err != nil {
		if errors.Is(err, store.ErrHostNotFound) {
			writeError(w, http.StatusNotFound, "no_such_host", "host not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to load host")
		return
	}
	if h.Disabled {
		writeError(w, http.StatusForbidden, "host_disabled", "host is disabled")
		return
	}
	listening, err := s.listening()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to probe listen")
		return
	}
	writeJSON(w, http.StatusOK, hostViewFrom(h, s.now().Unix(), s.agentTimeout(), listening, true))
}
