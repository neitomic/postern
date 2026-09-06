package agent

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	launchdLabel     = "com.postern.agent"
	launchdAgentPATH = "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"
)

func LaunchAgentPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func RenderLaunchAgentPlist(exe, home, logPath string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>`)
	b.WriteString(xmlEscape(launchdLabel))
	b.WriteString(`</string>
    <key>ProgramArguments</key>
    <array>
        <string>`)
	b.WriteString(xmlEscape(exe))
	b.WriteString(`</string>
        <string>agent</string>
        <string>run</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>5</integer>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>`)
	b.WriteString(xmlEscape(home))
	b.WriteString(`</string>
        <key>PATH</key>
        <string>`)
	b.WriteString(xmlEscape(launchdAgentPATH))
	b.WriteString(`</string>
        <key>AUTOSSH_GATETIME</key>
        <string>0</string>
        <key>AUTOSSH_PORT</key>
        <string>0</string>
    </dict>
    <key>StandardOutPath</key>
    <string>`)
	b.WriteString(xmlEscape(logPath))
	b.WriteString(`</string>
    <key>StandardErrorPath</key>
    <string>`)
	b.WriteString(xmlEscape(logPath))
	b.WriteString(`</string>
    <key>ProcessType</key>
    <string>Background</string>
</dict>
</plist>
`)
	return b.String()
}

func writeLaunchAgent(p Paths, exe, home string) error {
	path := LaunchAgentPlistPath(home)
	if err := mkdir(filepath.Dir(path)); err != nil {
		return err
	}
	body := RenderLaunchAgentPlist(exe, home, p.AgentLog())
	return writeFileAtomic(path, []byte(body), 0o644)
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func launchctlTarget() (domain, service string) {
	domain = fmt.Sprintf("gui/%d", os.Getuid())
	return domain, domain + "/" + launchdLabel
}
