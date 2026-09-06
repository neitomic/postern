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

func parseProcNetTCP(r io.Reader, min, max int) (map[int]struct{}, error) {
	out := make(map[int]struct{})
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
		out[port] = struct{}{}
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
