package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/neitomic/postern/internal/auth"
	"github.com/neitomic/postern/internal/names"
	"github.com/neitomic/postern/internal/store"
)

type issueRequest struct {
	TTL  string `json:"ttl"`
	Name string `json:"name"`
	Note string `json:"note"`
}

type tokenView struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Expires   int64   `json:"expires"`
	Used      bool    `json:"used"`
	Note      *string `json:"note"`
	BoundName *string `json:"bound_name"`
}

type issueResponse struct {
	OK    bool   `json:"ok"`
	Token string `json:"token"`
	tokenView
}

func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	ttl := s.Config.JoinTokenTTL.Duration()
	if req.TTL != "" {
		var err error
		ttl, err = time.ParseDuration(req.TTL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_ttl", "invalid ttl")
			return
		}
	}

	plain, row, err := auth.Issue(ttl, req.Name, req.Note)
	if err != nil {
		if errors.Is(err, auth.ErrTTL) {
			writeError(w, http.StatusBadRequest, "invalid_ttl", "ttl must be between 0 and 24h")
			return
		}
		if errors.Is(err, names.ErrInvalid) || errors.Is(err, names.ErrReserved) {
			writeError(w, http.StatusBadRequest, "invalid_name", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to issue token")
		return
	}
	if err := s.Store.InsertToken(&row); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to store token")
		return
	}
	writeJSON(w, http.StatusOK, issueResponse{
		OK:        true,
		Token:     plain,
		tokenView: tokenViewFrom(&row),
	})
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListTokens()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to list tokens")
		return
	}
	out := make([]tokenView, 0, len(list))
	for _, t := range list {
		out = append(out, tokenViewFrom(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.RevokeToken(id); err != nil {
		if errors.Is(err, store.ErrTokenNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to revoke token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func tokenViewFrom(t *store.Token) tokenView {
	return tokenView{
		ID:        t.ID,
		Kind:      t.Kind,
		Expires:   t.ExpiresAt,
		Used:      t.UsedAt != nil,
		Note:      t.Note,
		BoundName: t.BoundName,
	}
}
