package alloc

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

const (
	tcpListen       = "0A"
	addrLoopback    = "0100007F"
	addrUnspecified = "00000000"
)

type tcpSock struct {
	Port  int
	Inode uint64
}

func parseProcNetTCP(r io.Reader, min, max int) (map[int]struct{}, error) {
	list, err := parseProcNetTCPList(r, min, max)
	if err != nil {
		return nil, err
	}
	out := make(map[int]struct{}, len(list))
	for _, s := range list {
		out[s.Port] = struct{}{}
	}
	return out, nil
}

func parseProcNetTCPList(r io.Reader, min, max int) ([]tcpSock, error) {
	var out []tcpSock
	sc := bufio.NewScanner(r)
	if sc.Scan() {
		// skip header
	}
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		if !strings.EqualFold(fields[3], tcpListen) {
			continue
		}
		ip, port, ok := parseIPv4Local(fields[1])
		if !ok {
			continue
		}
		if ip != addrLoopback && ip != addrUnspecified {
			continue
		}
		if port < min || port > max {
			continue
		}
		var inode uint64
		if len(fields) > 9 {
			inode, _ = strconv.ParseUint(fields[9], 10, 64)
		}
		out = append(out, tcpSock{Port: port, Inode: inode})
	}
	return out, sc.Err()
}

func parseIPv4Local(s string) (ip string, port int, ok bool) {
	ip, p, found := strings.Cut(s, ":")
	if !found || len(ip) != 8 {
		return "", 0, false
	}
	n, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return "", 0, false
	}
	return strings.ToUpper(ip), int(n), true
}
