package agent

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const installedName = "postern"

// DefaultBinDir is ~/.local/bin — the stable path written into the agent unit.
func DefaultBinDir(home string) string {
	return filepath.Join(home, ".local", "bin")
}

func InstalledBinaryPath(home string) string {
	return filepath.Join(DefaultBinDir(home), installedName)
}

// InstallBinary copies src to dst (0755) via temp+rename so a running
// destination can be replaced. Same inode is a no-op.
func InstallBinary(src, dst string) error {
	var err error
	src, err = absExecutable(src)
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(src); err == nil {
		src = resolved
	}
	if !filepath.IsAbs(dst) {
		dst, err = filepath.Abs(dst)
		if err != nil {
			return err
		}
	}
	if sameFile(src, dst) {
		stripQuarantine(dst)
		return nil
	}
	if err := mkdir(filepath.Dir(dst)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), installedName+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := io.Copy(tmp, in); err != nil {
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	ok = true
	if err := os.Chmod(dst, 0o755); err != nil {
		return err
	}
	stripQuarantine(dst)
	return nil
}

func sameFile(a, b string) bool {
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

func stripQuarantine(path string) {
	if runtime.GOOS != "darwin" {
		return
	}
	_ = exec.Command("xattr", "-d", "com.apple.quarantine", path).Run()
}

func DirOnPATH(dir string) bool {
	dir = filepath.Clean(dir)
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p != "" && filepath.Clean(p) == dir {
			return true
		}
	}
	return false
}

func PATHHint(binDir string) string {
	if DirOnPATH(binDir) {
		return ""
	}
	return fmt.Sprintf("warning: %s is not on PATH; add it so `postern` resolves after this shell\n", binDir)
}

func AutosshHint() string {
	if _, err := exec.LookPath("autossh"); err == nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return "warning: autossh not on PATH; brew install autossh\n"
	default:
		return "warning: autossh not on PATH; sudo apt install autossh\n"
	}
}

func NextSteps(enrolled bool) string {
	var b strings.Builder
	if enrolled {
		b.WriteString("agent will keep the reverse tunnel up (autostart on login).\n")
		return b.String()
	}
	b.WriteString("next:\n")
	b.WriteString("  postern config set server debian@vps.example.net\n")
	b.WriteString("  postern config set name macbook\n")
	b.WriteString("  postern config accept-host-key\n")
	b.WriteString("  postern join --token psn_join_…\n")
	b.WriteString("the agent service is enabled and waits until this machine is enrolled.\n")
	return b.String()
}
