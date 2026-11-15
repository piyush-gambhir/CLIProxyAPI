package common

import "strings"

const ExternalToolPrefix = "external_"

var antigravityCollidingToolNames = map[string]struct{}{
	"read_file":    {},
	"write_file":   {},
	"execute_code": {},
}

func AntigravityToolNameToUpstream(name string) string {
	if antigravityIntrinsicBase(name) {
		return ExternalToolPrefix + name
	}
	return name
}

func antigravityIntrinsicBase(name string) bool {
	for strings.HasPrefix(name, ExternalToolPrefix) {
		name = strings.TrimPrefix(name, ExternalToolPrefix)
	}
	_, collides := antigravityCollidingToolNames[name]
	return collides
}

func AntigravityUpstreamToolNameToClient(name string) string {
	if strings.HasPrefix(name, ExternalToolPrefix) {
		base := strings.TrimPrefix(name, ExternalToolPrefix)
		if antigravityIntrinsicBase(base) {
			return base
		}
	}
	return name
}
