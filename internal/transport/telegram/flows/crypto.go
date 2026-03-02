package flows

import (
	"crypto/rand"
	"encoding/base64"
)

// NewSecureToken returns a cryptographically random, URL-safe base64 string
// generated from size random bytes.
func NewSecureToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
