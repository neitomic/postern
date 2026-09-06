package alloc

import (
	"runtime"
	"strings"
	"testing"
)

const sampleProcNetTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0898 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0000000000000000 100 0 0 10 0
   1: 00000000:0899 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2 1 0000000000000000 100 0 0 10 0
   2: 0101A8C0:089A 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 3 1 0000000000000000 100 0 0 10 0
   3: 0100007F:089B 0100007F:0016 01 00000000:00000000 00:00000000 00000000     0        0 4 1 0000000000000000 100 0 0 10 0
   4: 0100007F:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 5 1 0000000000000000 100 0 0 10 0
   5: 0100007F:08FC 00000000:0000 0a 00000000:00000000 00:00000000 00000000     0        0 6 1 0000000000000000 100 0 0 10 0
`

func TestParseProcNetTCPLoopbackAndWildcard(t *testing.T) {
	t.Parallel()
	got, err := parseProcNetTCP(strings.NewReader(sampleProcNetTCP), 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	// 0898=2200 127.0.0.1 LISTEN, 0899=2201 0.0.0.0 LISTEN, 08FC=2300 out of range
	if _, ok := got[2200]; !ok {
		t.Fatal("missing 127.0.0.1:2200 LISTEN")
	}
	if _, ok := got[2201]; !ok {
		t.Fatal("missing 0.0.0.0:2201 LISTEN")
	}
	if _, ok := got[2202]; ok {
		t.Fatal("192.168.1.1:2202 must not occupy")
	}
	if _, ok := got[2203]; ok {
		t.Fatal("ESTABLISHED 127.0.0.1:2203 must not occupy")
	}
	if _, ok := got[22]; ok {
		t.Fatal("port 22 is outside range")
	}
	if _, ok := got[2300]; ok {
		t.Fatal("2300 is outside range")
	}
}

func TestParseProcNetTCPEmpty(t *testing.T) {
	t.Parallel()
	got, err := parseProcNetTCP(strings.NewReader("  sl  local_address rem_address   st\n"), 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestProcProbeNonLinuxErrors(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "linux" {
		t.Skip("stub only")
	}
	_, err := ProcProbe().Listening(2200, 2299)
	if err == nil {
		t.Fatal("expected Linux-only error")
	}
}
