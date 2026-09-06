package alloc

import (
	"errors"
	"strings"

	"github.com/neitomic/postern/internal/names"
	"github.com/neitomic/postern/internal/store"
)

const (
	DefaultPortMin = 2200
	DefaultPortMax = 2299
)

var (
	ErrNameCollision  = errors.New("name_collision")
	ErrPortsExhausted = errors.New("ports_exhausted")
	ErrInvalidRange   = errors.New("invalid port range")
)

type Result struct {
	Port  int
	Reuse bool
}

type Store interface {
	HostByName(name string) (*store.Host, error)
	ListHosts() ([]*store.Host, error)
}

var _ Store = (*store.Store)(nil)

type ListenProbe interface {
	Listening(min, max int) (map[int]struct{}, error)
}

func Allocate(st Store, probe ListenProbe, name, fingerprint string, min, max int) (Result, error) {
	if min < 1 || max > 65535 || min > max {
		return Result{}, ErrInvalidRange
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if err := names.Valid(name); err != nil {
		return Result{}, err
	}

	existing, err := st.HostByName(name)
	if err != nil && !errors.Is(err, store.ErrHostNotFound) {
		return Result{}, err
	}
	if existing != nil {
		if existing.KeyFingerprint != fingerprint {
			return Result{}, ErrNameCollision
		}
		return Result{Port: existing.Port, Reuse: true}, nil
	}

	hosts, err := st.ListHosts()
	if err != nil {
		return Result{}, err
	}
	used := make(map[int]struct{}, len(hosts))
	for _, h := range hosts {
		used[h.Port] = struct{}{}
	}

	listening, err := probe.Listening(min, max)
	if err != nil {
		return Result{}, err
	}

	for p := min; p <= max; p++ {
		if _, ok := used[p]; ok {
			continue
		}
		if _, ok := listening[p]; ok {
			continue
		}
		return Result{Port: p, Reuse: false}, nil
	}
	return Result{}, ErrPortsExhausted
}
