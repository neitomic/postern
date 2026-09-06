package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const systemdUnitName = "postern-agent.service"

func SystemdUserUnitPath(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName)
}

func RenderSystemdUserUnit(exe string) string {
	return `[Unit]
Description=Postern reverse SSH tunnel agent

[Service]
Type=simple
ExecStart=` + systemdExecStart(exe) + `
Restart=always
RestartSec=5
Environment=AUTOSSH_GATETIME=0
Environment=AUTOSSH_PORT=0

[Install]
WantedBy=default.target
`
}

func systemdExecStart(exe string) string {
	return systemdQuote(exe) + " agent run"
}

func systemdQuote(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"'\\") {
		return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
	}
	return s
}

func writeSystemdUserUnit(exe, home string) error {
	path := SystemdUserUnitPath(home)
	if err := mkdir(filepath.Dir(path)); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(RenderSystemdUserUnit(exe)), 0o644)
}

func lingerWarning() string {
	user := os.Getenv("USER")
	if user == "" {
		return ""
	}
	out, err := runOutput("loginctl", "show-user", user, "-p", "Linger")
	if err != nil {
		return ""
	}
	if strings.Contains(out, "Linger=no") {
		return fmt.Sprintf("warning: lingering is disabled for %s; agent stops on logout. Enable with: loginctl enable-linger %s\n", user, user)
	}
	return ""
}
