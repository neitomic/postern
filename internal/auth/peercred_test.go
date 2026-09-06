package auth

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

const posternUID uint32 = 100
const posternGID uint32 = 200

var ids = Identities{PosternUID: posternUID, PosternGID: posternGID}

func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		peer Peer
		want Role
	}{
		{name: "root is admin", peer: Peer{UID: 0}, want: RoleAdmin},
		{name: "root in postern group is still admin", peer: Peer{UID: 0, GID: posternGID, Groups: []uint32{posternGID}}, want: RoleAdmin},
		{name: "postern uid is agent", peer: Peer{UID: posternUID, GID: posternGID, Groups: []uint32{posternGID}}, want: RoleAgent},
		{name: "postern uid without groups is agent", peer: Peer{UID: posternUID}, want: RoleAgent},
		{name: "supplementary postern group is admin", peer: Peer{UID: 1000, GID: 1000, Groups: []uint32{4, posternGID, 27}}, want: RoleAdmin},
		{name: "only postern supplementary group is admin", peer: Peer{UID: 1000, Groups: []uint32{posternGID}}, want: RoleAdmin},
		{name: "unrelated groups are none", peer: Peer{UID: 1000, Groups: []uint32{4, 27}}, want: RoleNone},
		{name: "no groups are none", peer: Peer{UID: 1000}, want: RoleNone},
		{name: "primary gid postern is not admin", peer: Peer{UID: 1000, GID: posternGID, Groups: nil}, want: RoleNone},
		{name: "primary gid postern with other groups is not admin", peer: Peer{UID: 1000, GID: posternGID, Groups: []uint32{4}}, want: RoleNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tc.peer, ids); got != tc.want {
				t.Fatalf("Classify = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyPosternUIDNeverAdminEvenIfRootUIDMatches(t *testing.T) {
	t.Parallel()
	p := Peer{UID: 0, Groups: []uint32{0}}
	got := Classify(p, Identities{PosternUID: 0, PosternGID: 0})
	if got != RoleAgent {
		t.Fatalf("Classify = %v, want agent when postern uid is 0", got)
	}
}

func TestParseStatusGroups(t *testing.T) {
	t.Parallel()
	body := `Name:	bash
Umask:	0022
Gid:	1000	1000	1000	1000
Groups:	4 24 27 30 46 110 200 1000
`
	got, err := parseStatusGroups(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{4, 24, 27, 30, 46, 110, 200, 1000}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParseStatusGroupsEmpty(t *testing.T) {
	t.Parallel()
	got, err := parseStatusGroups(strings.NewReader("Name:\tfoo\nGroups:\t\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestFromConnStubNonLinux(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "linux" {
		t.Skip("stub only")
	}
	_, err := FromConn(nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("FromConn = %v, want %v", err, ErrUnavailable)
	}
}
