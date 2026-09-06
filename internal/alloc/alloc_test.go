package alloc

import (
	"errors"
	"strings"
	"testing"

	"github.com/neitomic/postern/internal/names"
	"github.com/neitomic/postern/internal/store"
)

type memStore struct {
	hosts []*store.Host
}

func (m *memStore) HostByName(name string) (*store.Host, error) {
	for _, h := range m.hosts {
		if strings.EqualFold(h.Name, name) {
			return h, nil
		}
	}
	return nil, store.ErrHostNotFound
}

func (m *memStore) ListHosts() ([]*store.Host, error) {
	out := make([]*store.Host, len(m.hosts))
	copy(out, m.hosts)
	return out, nil
}

type mapProbe map[int]struct{}

func (m mapProbe) Listening(min, max int) (map[int]struct{}, error) {
	out := make(map[int]struct{})
	for p := range m {
		if p >= min && p <= max {
			out[p] = struct{}{}
		}
	}
	return out, nil
}

func occupied(ports ...int) mapProbe {
	m := make(mapProbe, len(ports))
	for _, p := range ports {
		m[p] = struct{}{}
	}
	return m
}

func host(name, fp string, port int, disabled bool) *store.Host {
	return &store.Host{
		Name:           name,
		Port:           port,
		KeyFingerprint: fp,
		Disabled:       disabled,
	}
}

func TestAllocateReuseSameNameFingerprint(t *testing.T) {
	t.Parallel()
	st := &memStore{hosts: []*store.Host{host("macbook", "SHA256:abc", 2223, false)}}
	got, err := Allocate(st, occupied(2223), "macbook", "SHA256:abc", 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 2223 || !got.Reuse {
		t.Fatalf("got %+v, want reuse of 2223", got)
	}
}

func TestAllocateCollisionDifferentFingerprint(t *testing.T) {
	t.Parallel()
	st := &memStore{hosts: []*store.Host{host("macbook", "SHA256:abc", 2223, false)}}
	_, err := Allocate(st, occupied(), "macbook", "SHA256:xyz", 2200, 2299)
	if !errors.Is(err, ErrNameCollision) {
		t.Fatalf("err = %v, want %v", err, ErrNameCollision)
	}
}

func TestAllocateSkipListenLoopbackAndWildcard(t *testing.T) {
	t.Parallel()
	// Fake probe occupies the same way IPv4 127.0.0.1 and 0.0.0.0 LISTEN do.
	st := &memStore{}
	got, err := Allocate(st, occupied(2200, 2201), "nuc", "SHA256:n", 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 2202 || got.Reuse {
		t.Fatalf("got %+v, want 2202", got)
	}
}

func TestAllocateSkipUsed(t *testing.T) {
	t.Parallel()
	st := &memStore{hosts: []*store.Host{
		host("a", "SHA256:a", 2200, false),
		host("b", "SHA256:b", 2202, false),
	}}
	got, err := Allocate(st, occupied(), "c", "SHA256:c", 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 2201 {
		t.Fatalf("port = %d, want 2201", got.Port)
	}
}

func TestAllocatePickLowest(t *testing.T) {
	t.Parallel()
	st := &memStore{}
	got, err := Allocate(st, occupied(), "pi", "SHA256:p", 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 2200 || got.Reuse {
		t.Fatalf("got %+v, want 2200", got)
	}
}

func TestAllocateExhaustion(t *testing.T) {
	t.Parallel()
	st := &memStore{hosts: []*store.Host{host("a", "SHA256:a", 2200, false)}}
	_, err := Allocate(st, occupied(2201), "b", "SHA256:b", 2200, 2201)
	if !errors.Is(err, ErrPortsExhausted) {
		t.Fatalf("err = %v, want %v", err, ErrPortsExhausted)
	}
}

func TestAllocateDisabledOwnsPort(t *testing.T) {
	t.Parallel()
	st := &memStore{hosts: []*store.Host{host("old", "SHA256:o", 2200, true)}}
	got, err := Allocate(st, occupied(), "new", "SHA256:n", 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 2201 {
		t.Fatalf("port = %d, want 2201 (disabled still owns 2200)", got.Port)
	}
}

func TestAllocateAfterRmSkipStillListen(t *testing.T) {
	t.Parallel()
	st := &memStore{}
	got, err := Allocate(st, occupied(2200), "macbook", "SHA256:new", 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 2201 {
		t.Fatalf("port = %d, want 2201 (still-LISTEN after rm)", got.Port)
	}
}

func TestAllocateInvalidName(t *testing.T) {
	t.Parallel()
	_, err := Allocate(&memStore{}, occupied(), "postern", "SHA256:x", 2200, 2299)
	if !errors.Is(err, names.ErrReserved) {
		t.Fatalf("err = %v, want %v", err, names.ErrReserved)
	}
	_, err = Allocate(&memStore{}, occupied(), "Bad.Name", "SHA256:x", 2200, 2299)
	if !errors.Is(err, names.ErrInvalid) {
		t.Fatalf("err = %v, want %v", err, names.ErrInvalid)
	}
}

func TestAllocateFoldsName(t *testing.T) {
	t.Parallel()
	st := &memStore{hosts: []*store.Host{host("macbook", "SHA256:abc", 2223, false)}}
	got, err := Allocate(st, occupied(), "MacBook", "SHA256:abc", 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Reuse || got.Port != 2223 {
		t.Fatalf("got %+v", got)
	}
}

func TestAllocateInvalidRange(t *testing.T) {
	t.Parallel()
	_, err := Allocate(&memStore{}, occupied(), "a", "fp", 2299, 2200)
	if !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidRange)
	}
}

func TestProcProbeStubNonLinux(t *testing.T) {
	t.Parallel()
	if ProcProbe() == nil {
		t.Fatal("ProcProbe returned nil")
	}
}
