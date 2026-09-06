package api

import (
	"encoding/json"
	"net/http"

	"github.com/neitomic/postern/internal/store"
)

type hostView struct {
	Name           string   `json:"name"`
	LoginUser      string   `json:"login_user"`
	Port           int      `json:"port"`
	KeyFingerprint string   `json:"key_fingerprint"`
	Tags           []string `json:"tags"`
	LastSeen       *int64   `json:"last_seen"`
	Disabled       bool     `json:"disabled"`
}

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListHosts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to list hosts")
		return
	}
	out := make([]hostView, 0, len(list))
	for _, h := range list {
		out = append(out, hostViewFrom(h))
	}
	writeJSON(w, http.StatusOK, out)
}

func hostViewFrom(h *store.Host) hostView {
	return hostView{
		Name:           h.Name,
		LoginUser:      h.LoginUser,
		Port:           h.Port,
		KeyFingerprint: h.KeyFingerprint,
		Tags:           parseTags(h.TagsJSON),
		LastSeen:       h.LastSeen,
		Disabled:       h.Disabled,
	}
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
