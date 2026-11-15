package synthesizer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher/diff"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)




type StableIDGenerator struct {
	counters map[string]int
}


func NewStableIDGenerator() *StableIDGenerator {
	return &StableIDGenerator{counters: make(map[string]int)}
}



func (g *StableIDGenerator) Next(kind string, parts ...string) (string, string) {
	if g == nil {
		return kind + ":000000000000", "000000000000"
	}
	hasher := sha256.New()
	hasher.Write([]byte(kind))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		hasher.Write([]byte{0})
		hasher.Write([]byte(trimmed))
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	if len(digest) < 12 {
		digest = fmt.Sprintf("%012s", digest)
	}
	short := digest[:12]
	key := kind + ":" + short
	index := g.counters[key]
	g.counters[key] = index + 1
	if index > 0 {
		short = fmt.Sprintf("%s-%d", short, index)
	}
	return fmt.Sprintf("%s:%s", kind, short), short
}







func ApplyAuthExcludedModelsMeta(auth *coreauth.Auth, perKey []string, authKind string) {
	if auth == nil {
		return
	}
	seen := make(map[string]struct{})
	for _, entry := range perKey {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			seen[strings.ToLower(trimmed)] = struct{}{}
		}
	}
	combined := make([]string, 0, len(seen))
	for k := range seen {
		combined = append(combined, k)
	}
	sort.Strings(combined)
	hash := diff.ComputeExcludedModelsHash(combined)
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	if hash != "" {
		auth.Attributes["excluded_models_hash"] = hash
	}

	if len(combined) > 0 {
		auth.Attributes["excluded_models"] = strings.Join(combined, ",")
	}
	if authKind != "" {
		auth.Attributes["auth_kind"] = authKind
	}
}



func addRequestRetryToMetadata(requestRetry *int, metadata map[string]any) {
	if requestRetry == nil || *requestRetry < 0 || metadata == nil {
		return
	}
	metadata["request_retry"] = *requestRetry
}


func addRequestScopedErrorsToMetadata(rules []config.RequestScopedErrorRule, metadata map[string]any) {
	if len(rules) == 0 || metadata == nil {
		return
	}
	metadata["request_scoped_errors"] = rules
}

func fingerprintProfileFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	for _, key := range []string{"fingerprint_profile", "fingerprint-profile"} {
		raw, _ := metadata[key].(string)
		if profile := strings.ToLower(strings.TrimSpace(raw)); profile != "" {
			return profile
		}
	}
	return ""
}




func applyFingerprintProfileAttribute(auth *coreauth.Auth, metadata map[string]any) {
	if auth == nil {
		return
	}
	profile := fingerprintProfileFromMetadata(metadata)
	if profile == "" {
		return
	}
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	auth.Attributes["fingerprint_profile"] = profile
}



func addConfigHeadersToAttrs(headers map[string]string, attrs map[string]string) {
	if len(headers) == 0 || attrs == nil {
		return
	}
	for hk, hv := range headers {
		key := strings.TrimSpace(hk)
		val := strings.TrimSpace(hv)
		if key == "" || val == "" {
			continue
		}
		attrs["header:"+key] = val
	}
}
