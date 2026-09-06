//go:build !linux

package auth

import "net"

func FromConn(c net.Conn) (Peer, error) {
	return Peer{}, ErrUnavailable
}
