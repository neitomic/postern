package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ReplaceManaged(existing, block string) (string, error) {
	block = strings.TrimRight(block, "\n") + "\n"
	begin := strings.Index(existing, BeginMarker)
	end := strings.Index(existing, EndMarker)
	if begin < 0 && end < 0 {
		if strings.TrimSpace(existing) == "" {
			return block, nil
		}
		return strings.TrimRight(existing, "\n") + "\n\n" + block, nil
	}
	if begin < 0 || end < 0 || end < begin {
		return "", fmt.Errorf("unbalanced POSTERN managed-block markers")
	}
	endAt := end + len(EndMarker)
	if endAt < len(existing) && existing[endAt] == '\n' {
		endAt++
	}
	prefix := existing[:begin]
	suffix := existing[endAt:]
	out := prefix + block
	if suffix != "" {
		out += suffix
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
	}
	return out, nil
}

// WritePrivate overwrites path with body at 0600. Used for postern ssh -F files
// so leftover Host stanzas cannot first-win over the regenerated jump identity.
func WritePrivate(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return writeAtomic(path, []byte(body), 0o600)
}

func WriteFile(path, block string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	created := false
	var existing []byte
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		created = true
	} else {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%s: not a regular file", path)
		}
		existing, err = os.ReadFile(path)
		if err != nil {
			return err
		}
	}
	out, err := ReplaceManaged(string(existing), block)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if !created {
		mode = fi.Mode().Perm()
	}
	return writeAtomic(path, []byte(out), mode)
}

func writeAtomic(path string, body []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}
