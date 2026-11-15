package antigravity

import (
	"fmt"
	"strings"
)



func CredentialFileName(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "antigravity.json"
	}
	return fmt.Sprintf("antigravity-%s.json", email)
}
