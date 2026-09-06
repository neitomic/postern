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
	exe, err = absExecutable(exe)
	if err != nil {
		return err
	}
	home, err = resolveHome(home)
	if err != nil {
		return err
	}
	if err := WriteSSHConfigs(p, st, cfg); err != nil {
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

func enableLaunchd(home string) error {
	plist := LaunchAgentPlistPath(home)
	if _, err := os.Stat(plist); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("not installed; run postern agent install")
		}
		return err
	}
	domain, service := launchctlTarget()
	// bootstrap errors when the job is already loaded; enable+kickstart still apply.
	_ = runQuiet("launchctl", "bootstrap", domain, plist)
	if err := runQuiet("launchctl", "enable", service); err != nil {
		return fmt.Errorf("launchctl enable: %w", err)
	}
	if err := runQuiet("launchctl", "kickstart", "-k", service); err != nil {
		return fmt.Errorf("launchctl kickstart: %w", err)
	}
	return nil
}

func disableLaunchd() error {
	_, service := launchctlTarget()
	// bootout fails if the job is not loaded; disable must still succeed.
	_ = runQuiet("launchctl", "bootout", service)
	return nil
}

func enableSystemd() error {
	if err := runQuiet("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := runQuiet("systemctl", "--user", "enable", "--now", systemdUnitName); err != nil {
		return fmt.Errorf("systemctl enable: %w", err)
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
