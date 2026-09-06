package agent

import (
	"fmt"
	"strings"

	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/names"
)

func WriteSSHConfigs(p Paths, st State, cfg config.Client) error {
	tunnel, err := formatSSHConfig(p, st, cfg, true)
	if err != nil {
		return err
	}
	rpc, err := formatSSHConfig(p, st, cfg, false)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(p.SSHConfig(), []byte(tunnel), 0o600); err != nil {
		return err
	}
	return writeFileAtomic(p.SSHConfigRPC(), []byte(rpc), 0o600)
}

func formatSSHConfig(p Paths, st State, cfg config.Client, withForward bool) (string, error) {
	port := 22
	if cfg.Server != "" {
		_, _, pnum, err := config.ParseServer(cfg.Server)
		if err != nil {
			return "", err
		}
		port = pnum
	}
	local := cfg.LocalSSHPort
	if local == 0 {
		local = 22
	}
	if err := names.ValidLoginUser(st.TunnelUser); err != nil {
		return "", err
	}
	if err := names.ValidHostname(st.VPSHostname); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Host postern-tunnel\n")
	fmt.Fprintf(&b, "    HostName %s\n", st.VPSHostname)
	fmt.Fprintf(&b, "    User %s\n", st.TunnelUser)
	fmt.Fprintf(&b, "    Port %d\n", port)
	fmt.Fprintf(&b, "    IdentityFile %s\n", p.IdentityFile())
	fmt.Fprintf(&b, "    IdentitiesOnly yes\n")
	fmt.Fprintf(&b, "    BatchMode yes\n")
	fmt.Fprintf(&b, "    ExitOnForwardFailure yes\n")
	fmt.Fprintf(&b, "    ServerAliveInterval 30\n")
	fmt.Fprintf(&b, "    ServerAliveCountMax 3\n")
	fmt.Fprintf(&b, "    StrictHostKeyChecking yes\n")
	fmt.Fprintf(&b, "    UserKnownHostsFile %s\n", p.KnownHosts())
	fmt.Fprintf(&b, "    GlobalKnownHostsFile /dev/null\n")
	if withForward {
		fmt.Fprintf(&b, "    RemoteForward 127.0.0.1:%d 127.0.0.1:%d\n", st.Port, local)
	}
	return b.String(), nil
}
