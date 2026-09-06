package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func absExecutable(exe string) (string, error) {
	var err error
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return "", err
		}
	}
	if !filepath.IsAbs(exe) {
		exe, err = filepath.Abs(exe)
		if err != nil {
			return "", err
		}
	}
	return exe, nil
}

func resolveHome(home string) (string, error) {
	if home != "" {
		return home, nil
	}
	return os.UserHomeDir()
}

func Install(p Paths, exe, home string) error {
	cfg, st, err := loadEnrolled(p)
	if err != nil {
		return err
	}
	if err := WriteSSHConfigs(p, st, cfg); err != nil {
		return err
	}
	return WriteAgentUnit(p, exe, home)
}

// WriteAgentUnit writes the LaunchAgent plist or systemd --user unit. It does
// not require enrollment; agent run waits until state.json exists.
func WriteAgentUnit(p Paths, exe, home string) error {
	var err error
	exe, err = absExecutable(exe)
	if err != nil {
		return err
	}
	home, err = resolveHome(home)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return writeLaunchAgent(p, exe, home)
	case "linux":
		return writeSystemdUserUnit(exe, home)
	default:
		return fmt.Errorf("agent install is not supported on %s", runtime.GOOS)
	}
}

func Uninstall(home string) error {
	home, err := resolveHome(home)
	if err != nil {
		return err
	}
	_ = Disable(home, io.Discard)
	switch runtime.GOOS {
	case "darwin":
		path := LaunchAgentPlistPath(home)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	case "linux":
		path := SystemdUserUnitPath(home)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = runQuiet("systemctl", "--user", "daemon-reload")
		return nil
	default:
		return fmt.Errorf("agent uninstall is not supported on %s", runtime.GOOS)
	}
}

func Enable(home string, stderr io.Writer) error {
	home, err := resolveHome(home)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return enableLaunchd(home)
	case "linux":
		if err := enableSystemd(); err != nil {
			return err
		}
		if w := lingerWarning(); w != "" && stderr != nil {
			fmt.Fprint(stderr, w)
		}
		return nil
	default:
		return fmt.Errorf("agent enable is not supported on %s", runtime.GOOS)
	}
}

func Disable(home string, stderr io.Writer) error {
	home, err := resolveHome(home)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return disableLaunchd()
	case "linux":
		return disableSystemd()
	default:
		return fmt.Errorf("agent disable is not supported on %s", runtime.GOOS)
	}
}

func Status(home string, stdout, stderr io.Writer) error {
	home, err := resolveHome(home)
	if err != nil {
		return err
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	switch runtime.GOOS {
	case "darwin":
		path := LaunchAgentPlistPath(home)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("not installed; run postern agent install")
			}
			return err
		}
		_, service := launchctlTarget()
		return runIO(stdout, stderr, "launchctl", "print", service)
	case "linux":
		path := SystemdUserUnitPath(home)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("not installed; run postern agent install")
			}
			return err
		}
		return runIO(stdout, stderr, "systemctl", "--user", "status", systemdUnitName)
	default:
		return fmt.Errorf("agent status is not supported on %s", runtime.GOOS)
	}
}

func launchdEnableArgs(home string) [][]string {
	plist := LaunchAgentPlistPath(home)
	domain, service := launchctlTarget()
	return [][]string{
		{"bootout", service},
		{"enable", service},
		{"bootstrap", domain, plist},
		{"kickstart", "-k", service},
	}
}

func launchdDisableArgs() [][]string {
	_, service := launchctlTarget()
	return [][]string{
		{"disable", service},
		{"bootout", service},
	}
}

func systemdEnableArgs(wasActive bool) [][]string {
	steps := [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", systemdUnitName},
	}
	if wasActive {
		steps = append(steps, []string{"--user", "restart", systemdUnitName})
	}
	return steps
}

func enableLaunchd(home string) error {
	plist := LaunchAgentPlistPath(home)
	if _, err := os.Stat(plist); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("not installed; run postern agent install")
		}
		return err
	}
	// bootout (reload plist), enable before bootstrap (disabled labels reject bootstrap), kickstart.
	for i, args := range launchdEnableArgs(home) {
		if err := runQuiet("launchctl", args...); err != nil {
			if i == 0 {
				continue
			}
			return fmt.Errorf("launchctl %s: %w", args[0], err)
		}
	}
	return nil
}

func disableLaunchd() error {
	// disable is the persistent gui-domain flag; bootout only stops this login.
	for _, args := range launchdDisableArgs() {
		_ = runQuiet("launchctl", args...)
	}
	return nil
}

func enableSystemd() error {
	wasActive := runQuiet("systemctl", "--user", "is-active", "--quiet", systemdUnitName) == nil
	for _, args := range systemdEnableArgs(wasActive) {
		if err := runQuiet("systemctl", args...); err != nil {
			return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

func disableSystemd() error {
	_ = runQuiet("systemctl", "--user", "disable", "--now", systemdUnitName)
	return nil
}

func runQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := bytes.TrimSpace(out)
		if len(msg) > 0 {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runIO(stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func runOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
