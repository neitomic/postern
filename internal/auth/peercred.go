package auth

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
)

var ErrUnavailable = errors.New("peercred is Linux-only")

type Role int

const (
	RoleNone Role = iota
	RoleAdmin
	RoleAgent
)

type Peer struct {
	PID    int
	UID    uint32
	GID    uint32 // primary gid only; never used for the postern group check
	Groups []uint32
}

type Identities struct {
	PosternUID uint32
	PosternGID uint32
}

type peerCtxKey struct{}

func WithPeer(ctx context.Context, p Peer) context.Context {
	return context.WithValue(ctx, peerCtxKey{}, p)
}

func PeerFromContext(ctx context.Context) (Peer, bool) {
	p, ok := ctx.Value(peerCtxKey{}).(Peer)
	return p, ok
}

func Classify(p Peer, id Identities) Role {
	if p.UID == id.PosternUID {
		return RoleAgent
	}
	if p.UID == 0 {
		return RoleAdmin
	}
	for _, g := range p.Groups {
		if g == id.PosternGID {
			return RoleAdmin
		}
	}
	return RoleNone
}

func parseStatusGroups(r io.Reader) ([]uint32, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "Groups:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		out := make([]uint32, 0, len(fields))
		for _, f := range fields {
			v, err := strconv.ParseUint(f, 10, 32)
			if err != nil {
				return nil, err
			}
			out = append(out, uint32(v))
		}
		return out, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("Groups: not found")
}
