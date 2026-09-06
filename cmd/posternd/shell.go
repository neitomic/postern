package main

import (
	"os"

	"github.com/neitomic/postern/internal/rpc"
)

// shellMain is the argv0 posternd-shell path. It must not use cobra:
// sshd execs login_shell -c <ForceCommand>, so argv is
// [posternd-shell, -c, /usr/bin/posternd-shell] and cobra would treat -c as a flag.
func shellMain(argv []string) int {
	return rpc.Env{
		Args:   argv,
		Orig:   os.Getenv("SSH_ORIGINAL_COMMAND"),
		Name:   os.Getenv("POSTERN_NAME"),
		Socket: os.Getenv("POSTERND_SOCKET"),
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}.Main()
}
