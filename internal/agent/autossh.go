package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	tunnelHost      = "postern-tunnel"
	agentAPICommand = "postern-agent-api"
	heartbeatBody   = `{"v":1,"op":"heartbeat"}`
)

func LookPathAutossh() (string, error) {
	if p, err := exec.LookPath("autossh"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("install autossh")
}

func AutosshArgs(sshConfig string) []string {
	return []string{"-M", "0", "-N", "-F", sshConfig, tunnelHost}
}

func HeartbeatArgs(rpcConfig string) []string {
	return []string{"-T", "-F", rpcConfig, tunnelHost, agentAPICommand}
}

func autosshEnv(parent []string) []string {
	out := make([]string, 0, len(parent)+2)
	for _, e := range parent {
		if strings.HasPrefix(e, "AUTOSSH_GATETIME=") || strings.HasPrefix(e, "AUTOSSH_PORT=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "AUTOSSH_GATETIME=0", "AUTOSSH_PORT=0")
}

func parseRemoteForwardListenPort(body []byte) (int, error) {
	found := false
	port := 0
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "RemoteForward") {
			continue
		}
		listen := fields[1]
		i := strings.LastIndex(listen, ":")
		if i < 0 || i == len(listen)-1 {
			return 0, fmt.Errorf("RemoteForward missing listen port: %s", trimmed)
		}
		n, err := strconv.Atoi(listen[i+1:])
		if err != nil || n < 1 || n > 65535 {
			return 0, fmt.Errorf("RemoteForward missing listen port: %s", trimmed)
		}
		if found {
			return 0, fmt.Errorf("multiple RemoteForward lines")
		}
		found = true
		port = n
	}
	if !found {
		return 0, fmt.Errorf("RemoteForward missing")
	}
	return port, nil
}

func checkRemoteForward(path string, wantPort int) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	got, err := parseRemoteForwardListenPort(body)
	if err != nil {
		return err
	}
	if got != wantPort {
		return fmt.Errorf("RemoteForward listen port %d != state.json.port %d", got, wantPort)
	}
	return nil
}
