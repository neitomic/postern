package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

func GenerateKey(privPath, comment string) error {
	if _, err := os.Stat(privPath); err == nil {
		return fmt.Errorf("key already exists: %s", privPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(privPath, pem.EncodeToMemory(block), 0o600); err != nil {
		return err
	}
	return writePublicKey(privPath+".pub", sshPub, comment)
}

func EnsureKey(privPath, comment string) error {
	pubPath := privPath + ".pub"
	if _, err := os.Stat(privPath); err == nil {
		if _, err := os.Stat(pubPath); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		return writePubFromPrivate(privPath, pubPath, comment)
	} else if !os.IsNotExist(err) {
		return err
	}
	return GenerateKey(privPath, comment)
}

func writePubFromPrivate(privPath, pubPath, comment string) error {
	raw, err := os.ReadFile(privPath)
	if err != nil {
		return err
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return err
	}
	return writePublicKey(pubPath, signer.PublicKey(), comment)
}

func writePublicKey(pubPath string, pub ssh.PublicKey, comment string) error {
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		line += " " + comment
	}
	line += "\n"
	return writeFileAtomic(pubPath, []byte(line), 0o644)
}

func KeyComment(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "postern"
	}
	return "postern:" + name
}
