package auth

import "golang.org/x/crypto/ssh"

// Fingerprint is OpenSSH SHA256: + unpadded base64 of sha256(wire pubkey).
func Fingerprint(key ssh.PublicKey) string {
	return ssh.FingerprintSHA256(key)
}
