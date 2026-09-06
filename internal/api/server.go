package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/neitomic/postern/internal/alloc"
	"github.com/neitomic/postern/internal/auth"
	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/store"
	"github.com/neitomic/postern/internal/version"
)

const socketMode = 0o660

type Server struct {
	Store      *store.Store
	Config     config.Posternd
	PosternUID uint32
	PosternGID uint32
	Probe      alloc.ListenProbe
	LookupPeer func(net.Conn) (auth.Peer, error)
	RenderKeys func(hosts []*store.Host) error
	Now        func() time.Time
	PortPIDs   func(min, max int) map[int]int
	KillListen func(port int) error
	// ListenGoneTries/Sleep override the post-SIGTERM LISTEN poll. Zero tries = default.
	ListenGoneTries int
	ListenGoneSleep time.Duration
	keysMu          sync.Mutex // list+write+compensate; concurrent enrolls otherwise clobber authorized_keys
}

type errorBody struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

func Listen(socketPath string) (net.Listener, error) {
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, socketMode); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("POST /v1/tokens", s.requireAdmin(s.handleIssueToken))
	mux.HandleFunc("GET /v1/tokens", s.requireAdmin(s.handleListTokens))
	mux.HandleFunc("DELETE /v1/tokens/{id}", s.requireAdmin(s.handleRevokeToken))
	mux.HandleFunc("POST /v1/enroll", s.requireAdmin(s.handleEnroll))
	mux.HandleFunc("GET /v1/hosts", s.requireAdmin(s.handleListHosts))
	mux.HandleFunc("GET /v1/hosts/{name}", s.requireAdmin(s.handleShowHost))
	mux.HandleFunc("DELETE /v1/hosts/{name}", s.requireAdmin(s.handleDeleteHost))
	mux.HandleFunc("POST /v1/hosts/{name}/disable", s.requireAdmin(s.handleDisableHost))
	mux.HandleFunc("POST /v1/hosts/{name}/enable", s.requireAdmin(s.handleEnableHost))
	mux.HandleFunc("POST /v1/hosts/{name}/rename", s.requireAdmin(s.handleRenameHost))
	mux.HandleFunc("POST /v1/hosts/{name}/rekey", s.requireAdmin(s.handleRekeyHost))
	mux.HandleFunc("POST /v1/gc", s.requireAdmin(s.handleGC))
	mux.HandleFunc("GET /v1/ports", s.requireAdmin(s.handlePortsAudit))
	mux.HandleFunc("POST /v1/authorized-keys/render", s.requireAdmin(s.handleRenderKeys))
	mux.HandleFunc("POST /v1/agent/heartbeat", s.requireAgent(s.notImplemented))
	mux.HandleFunc("GET /v1/agent/self", s.requireAgent(s.notImplemented))
	mux.HandleFunc("/", s.notFound)
	return s.auth(mux)
}

func (s *Server) HTTPServer() *http.Server {
	lookup := s.LookupPeer
	if lookup == nil {
		lookup = auth.FromConn
	}
	return &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			p, err := lookup(c)
			if err != nil {
				return ctx
			}
			return auth.WithPeer(ctx, p)
		},
	}
}

func (s *Server) auth(next http.Handler) http.Handler {
	ids := auth.Identities{PosternUID: s.PosternUID, PosternGID: s.PosternGID}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth.PeerFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusForbidden, "forbidden", "not authorized")
			return
		}
		role := auth.Classify(p, ids)
		if role == auth.RoleNone {
			writeError(w, http.StatusForbidden, "forbidden", "not authorized")
			return
		}
		ctx := context.WithValue(r.Context(), roleKey{}, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type roleKey struct{}

func roleFrom(ctx context.Context) auth.Role {
	r, _ := ctx.Value(roleKey{}).(auth.Role)
	return r
}

func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if roleFrom(r.Context()) != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "admin required")
			return
		}
		h(w, r)
	}
}

func (s *Server) requireAgent(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if roleFrom(r.Context()) != auth.RoleAgent {
			writeError(w, http.StatusForbidden, "forbidden", "agent required")
			return
		}
		h(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version.Version})
}

func (s *Server) notImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "not implemented")
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "no such route")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{OK: false, Error: code, Message: msg})
}
