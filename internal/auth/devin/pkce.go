package devin

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/MoeForge/moego/base64"
)

type PKCECodes struct {
	CodeVerifier  string
	CodeChallenge string
}

func GeneratePKCECodes() (*PKCECodes, error) {
	bytes := make([]byte, 64)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}

	verifier := base64.RawURLEncoding.EncodeToString(bytes)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	return &PKCECodes{
		CodeVerifier:  verifier,
		CodeChallenge: challenge,
	}, nil
}
