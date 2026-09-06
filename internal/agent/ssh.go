package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/neitomic/postern/internal/config"
)

func ControlSSHArgs(cfg config.Client, knownHosts string, remote ...string) ([]string, error) {
	user, host, port, err := config.ParseServer(cfg.Server)
	if err != nil {
		return nil, err
	}
	args := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "ForwardAgent=no",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"-o", "GlobalKnownHostsFile=/dev/null",
	}
	if cfg.IdentityFile != "" {
		args = append(args, "-o", "IdentityFile="+cfg.IdentityFile, "-o", "IdentitiesOnly=yes")
	}
	if port != 22 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	target := host
	if user != "" {
		target = user + "@" + host
	}
	args = append(args, target)
	args = append(args, remote...)
	return args, nil
}

func EnrollMachineArgs(cfg config.Client, knownHosts string) ([]string, error) {
	path := cfg.PosterndPath
	if path == "" {
		path = "/usr/bin/posternd"
	}
	return ControlSSHArgs(cfg, knownHosts, path, "enroll", "--json")
}

func SubmitProbeArgs(cfg config.Client, knownHosts string) ([]string, error) {
	return ControlSSHArgs(cfg, knownHosts, "true")
}

func AcceptHostKey(knownHosts, host string, port int) error {
	args := []string{"-t", "ed25519,ecdsa,rsa"}
	if port != 22 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	args = append(args, host)
	cmd := exec.Command("ssh-keyscan", args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return fmt.Errorf("ssh-keyscan: %w: %s", err, bytesToOneLine(ee.Stderr))
		}
		return fmt.Errorf("ssh-keyscan: %w", err)
	}
	if !hasKnownHostsKey(out) {
		return fmt.Errorf("ssh-keyscan returned no host keys for %s", host)
	}
	return writeFileAtomic(knownHosts, out, 0o644)
}

func hasKnownHostsKey(out []byte) bool {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

func bytesToOneLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	return strings.ReplaceAll(s, "\n", " ")
}

func LookPathSSH() (string, error) {
	if p, err := exec.LookPath("ssh"); err == nil {
		return p, nil
	}
	const fallback = "/usr/bin/ssh"
	if _, err := os.Stat(fallback); err == nil {
		return fallback, nil
	}
	return "", fmt.Errorf("ssh not found")
}
