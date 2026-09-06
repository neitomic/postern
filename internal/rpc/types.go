package rpc

import (
	"io"
	"net/http"
)

// AgentCommand is the only SSH_ORIGINAL_COMMAND that maps to JSON RPC.
const AgentCommand = "postern-agent-api"

const headerPosternName = "X-Postern-Name"

// Env is the posternd-shell dispatcher input. Args after -c are ignored
// (ForceCommand path): identity is SSH_ORIGINAL_COMMAND, not argv.
type Env struct {
	Args   []string
	Orig   string
	Name   string
	Socket string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Isatty func() bool
	Hold   func()
	DoAPI  func() int
	HTTP   func(*http.Request) (*http.Response, error)
}

type rpcRequest struct {
	V  int    `json:"v"`
	Op string `json:"op"`
}

type rpcError struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Message string `json:"message"`
}
