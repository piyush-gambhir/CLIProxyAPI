package auth

import (
	"encoding/json"
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
)

const oauthModelAliasesAttributeKey = "model_aliases"

type modelAliasEntry interface {
	GetName() string
	GetAlias() string
	GetForceMapping() bool
}

type oauthModelAliasEntry struct {
	upstreamModel string
	configAlias   string
	forceMapping  bool
}

type oauthModelAliasTable struct {
	reverse map[string]map[string]oauthModelAliasEntry
}

type OAuthModelAliasResult struct {
	UpstreamModel string
	ForceMapping  bool
	OriginalAlias string
}

func compileOAuthModelAliasTable(aliases map[string][]internalconfig.OAuthModelAlias) *oauthModelAliasTable {
	if len(aliases) == 0 {
		return &oauthModelAliasTable{}
	}
	out := &oauthModelAliasTable{
		reverse: make(map[string]map[string]oauthModelAliasEntry, len(aliases)),
	}
	for rawChannel, entries := range aliases {
		channel := strings.ToLower(strings.TrimSpace(rawChannel))
		if channel == "" || len(entries) == 0 {
			continue
		}
		rev := make(map[string]oauthModelAliasEntry, len(entries))
		for _, entry := range entries {
			name := strings.TrimSpace(entry.Name)
			alias := strings.TrimSpace(entry.Alias)
			if name == "" || alias == "" {
				continue
			}
			if strings.EqualFold(name, alias) {
				continue
			}
			aliasKey := strings.ToLower(alias)
			if _, exists := rev[aliasKey]; exists {
				continue
			}
			rev[aliasKey] = oauthModelAliasEntry{
				upstreamModel: name,
				configAlias:   alias,
				forceMapping:  entry.ForceMapping,
			}
		}
		if len(rev) > 0 {
			out.reverse[channel] = rev
		}
	}
	if len(out.reverse) == 0 {
		out.reverse = nil
	}
	return out
}

func (m *Manager) SetOAuthModelAlias(aliases map[string][]internalconfig.OAuthModelAlias) {
	if m == nil {
		return
	}
	table := compileOAuthModelAliasTable(aliases)

	if table == nil {
		table = &oauthModelAliasTable{}
	}
	m.publishOAuthModelAliasAndReconcileStates(nil, table)
}

type oauthResolvedAliasRoute struct {
	aliasKey  string
	targetKey string
}

func (m *Manager) configuredOAuthAliasRoutes(auth *Auth) []oauthResolvedAliasRoute {
	if m == nil || auth == nil {
		return nil
	}
	channel := modelAliasChannel(auth)
	if channel == "" {
		return nil
	}
	seenAliases := make(map[string]struct{})
	var routes []oauthResolvedAliasRoute
	addAlias := func(alias string) {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			return
		}
		aliasLookup := canonicalModelLookupKey(alias)
		if aliasLookup == "" {
			return
		}
		if _, exists := seenAliases[aliasLookup]; exists {
			return
		}
		seenAliases[aliasLookup] = struct{}{}
		aliasKey := canonicalModelKey(alias)
		if targetKey := m.selectionModelKeyForAuth(auth, alias); targetKey != "" && !sameCanonicalModelKey(aliasKey, targetKey) {
			routes = append(routes, oauthResolvedAliasRoute{
				aliasKey:  aliasKey,
				targetKey: targetKey,
			})
		}
	}
	for _, entry := range OAuthModelAliasesFromAttributes(authAttributes(auth)) {
		addAlias(entry.Alias)
	}
	raw := m.oauthModelAlias.Load()
	if table, _ := raw.(*oauthModelAliasTable); table != nil && table.reverse != nil {
		for _, entry := range table.reverse[channel] {
			addAlias(entry.configAlias)
		}
	}
	return routes
}

func (m *Manager) applyOAuthModelAlias(auth *Auth, requestedModel string) string {
	upstreamModel := m.resolveOAuthUpstreamModel(auth, requestedModel)
	if upstreamModel == "" {
		return requestedModel
	}
	return upstreamModel
}

func modelAliasLookupCandidates(requestedModel string) (thinking.SuffixResult, []string) {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return thinking.SuffixResult{}, nil
	}
	requestResult := thinking.ParseSuffix(requestedModel)
	base := requestResult.ModelName
	if base == "" {
		base = requestedModel
	}
	candidates := []string{requestedModel}
	if base != requestedModel {
		candidates = append(candidates, base)
	}
	return requestResult, candidates
}

func preserveResolvedModelSuffix(resolved string, requestResult thinking.SuffixResult) string {
	resolved = strings.TrimSpace(resolved)
	if resolved == "" {
		return ""
	}
	if thinking.ParseSuffix(resolved).HasSuffix {
		return resolved
	}
	if requestResult.HasSuffix && requestResult.RawSuffix != "" {
		return resolved + "(" + requestResult.RawSuffix + ")"
	}
	return resolved
}

func oauthModelAliasForceMappingResponseModel(configAlias string) string {
	return strings.TrimSpace(configAlias)
}

func resolveModelAliasPoolFromConfigModels(requestedModel string, models []modelAliasEntry) []string {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return nil
	}
	if len(models) == 0 {
		return nil
	}

	requestResult, candidates := modelAliasLookupCandidates(requestedModel)
	if len(candidates) == 0 {
		return nil
	}

	for _, candidate := range candidates {
		out := make([]string, 0)
		seen := make(map[string]struct{})
		for i := range models {
			name := strings.TrimSpace(models[i].GetName())
			alias := strings.TrimSpace(models[i].GetAlias())
			if candidate == "" || alias == "" || !strings.EqualFold(alias, candidate) {
				continue
			}
			resolved := candidate
			if name != "" {
				resolved = name
			}
			resolved = preserveResolvedModelSuffix(resolved, requestResult)
			key := strings.ToLower(strings.TrimSpace(resolved))
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, resolved)
		}
		if len(out) > 0 {
			return out
		}
	}

	for _, candidate := range candidates {
		for i := range models {
			name := strings.TrimSpace(models[i].GetName())
			if candidate == "" || name == "" || !strings.EqualFold(name, candidate) {
				continue
			}
			return []string{preserveResolvedModelSuffix(name, requestResult)}
		}
	}
	return nil
}

func resolveModelAliasFromConfigModels(requestedModel string, models []modelAliasEntry) string {
	resolved := resolveModelAliasPoolFromConfigModels(requestedModel, models)
	if len(resolved) > 0 {
		return resolved[0]
	}
	return ""
}

func resolveModelAliasResultFromConfigModels(requestedModel string, models []modelAliasEntry) OAuthModelAliasResult {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" || len(models) == 0 {
		return OAuthModelAliasResult{}
	}
	requestResult, candidates := modelAliasLookupCandidates(requestedModel)
	if len(candidates) == 0 {
		return OAuthModelAliasResult{}
	}
	baseModel := requestResult.ModelName
	if baseModel == "" {
		baseModel = requestedModel
	}
	for _, candidate := range candidates {
		key := strings.TrimSpace(candidate)
		if key == "" {
			continue
		}
		for i := range models {
			original := strings.TrimSpace(models[i].GetName())
			alias := strings.TrimSpace(models[i].GetAlias())
			if original == "" || alias == "" || !strings.EqualFold(alias, key) {
				continue
			}
			if strings.EqualFold(original, baseModel) {
				if !models[i].GetForceMapping() {
					return OAuthModelAliasResult{}
				}
				return OAuthModelAliasResult{
					UpstreamModel: preserveResolvedModelSuffix(original, requestResult),
					ForceMapping:  models[i].GetForceMapping(),
					OriginalAlias: oauthModelAliasForceMappingResponseModel(alias),
				}
			}
			originalAlias := requestedModel
			if models[i].GetForceMapping() {
				originalAlias = oauthModelAliasForceMappingResponseModel(alias)
			}
			return OAuthModelAliasResult{
				UpstreamModel: preserveResolvedModelSuffix(original, requestResult),
				ForceMapping:  models[i].GetForceMapping(),
				OriginalAlias: originalAlias,
			}
		}
	}
	return OAuthModelAliasResult{}
}

func (m *Manager) resolveOAuthUpstreamModel(auth *Auth, requestedModel string) string {
	result := m.resolveOAuthModelAliasWithResult(auth, requestedModel)
	return result.UpstreamModel
}

func (m *Manager) resolveOAuthModelAliasWithResult(auth *Auth, requestedModel string) OAuthModelAliasResult {
	channel := modelAliasChannel(auth)
	if channel == "" {
		return OAuthModelAliasResult{}
	}
	if result := resolveUpstreamModelFromAliases(OAuthModelAliasesFromAttributes(authAttributes(auth)), requestedModel); result.UpstreamModel != "" {
		return result
	}
	return resolveUpstreamModelFromAliasTable(m, auth, requestedModel, channel)
}

func authAttributes(auth *Auth) map[string]string {
	if auth == nil {
		return nil
	}
	return auth.Attributes
}

func SetOAuthModelAliasesAttribute(auth *Auth, aliases []internalconfig.OAuthModelAlias) {
	if auth == nil {
		return
	}
	aliases = sanitizeOAuthModelAliases(aliases)
	if len(aliases) == 0 {
		return
	}
	data, errMarshal := json.Marshal(aliases)
	if errMarshal != nil {
		return
	}
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	auth.Attributes[oauthModelAliasesAttributeKey] = string(data)
}

func OAuthModelAliasesFromAttributes(attributes map[string]string) []internalconfig.OAuthModelAlias {
	if len(attributes) == 0 {
		return nil
	}
	raw := strings.TrimSpace(attributes[oauthModelAliasesAttributeKey])
	if raw == "" {
		return nil
	}
	var aliases []internalconfig.OAuthModelAlias
	if errUnmarshal := json.Unmarshal([]byte(raw), &aliases); errUnmarshal != nil {
		return nil
	}
	return sanitizeOAuthModelAliases(aliases)
}

func sanitizeOAuthModelAliases(aliases []internalconfig.OAuthModelAlias) []internalconfig.OAuthModelAlias {
	if len(aliases) == 0 {
		return nil
	}
	cfg := internalconfig.Config{
		OAuthModelAlias: map[string][]internalconfig.OAuthModelAlias{
			"auth": aliases,
		},
	}
	cfg.SanitizeOAuthModelAlias()
	clean := cfg.OAuthModelAlias["auth"]
	if len(clean) == 0 {
		return nil
	}
	return append([]internalconfig.OAuthModelAlias(nil), clean...)
}

func resolveUpstreamModelFromAliases(aliases []internalconfig.OAuthModelAlias, requestedModel string) OAuthModelAliasResult {
	if len(aliases) == 0 {
		return OAuthModelAliasResult{}
	}
	requestResult, candidates := modelAliasLookupCandidates(requestedModel)
	if len(candidates) == 0 {
		return OAuthModelAliasResult{}
	}
	baseModel := requestResult.ModelName
	if baseModel == "" {
		baseModel = strings.TrimSpace(requestedModel)
	}
	for _, candidate := range candidates {
		key := strings.TrimSpace(candidate)
		if key == "" {
			continue
		}
		for _, entry := range aliases {
			original := strings.TrimSpace(entry.Name)
			alias := strings.TrimSpace(entry.Alias)
			if original == "" || alias == "" || !strings.EqualFold(alias, key) {
				continue
			}
			if strings.EqualFold(original, baseModel) {
				if !entry.ForceMapping {
					return OAuthModelAliasResult{}
				}
				return OAuthModelAliasResult{
					UpstreamModel: preserveResolvedModelSuffix(original, requestResult),
					ForceMapping:  entry.ForceMapping,
					OriginalAlias: oauthModelAliasForceMappingResponseModel(alias),
				}
			}
			originalAlias := requestedModel
			if entry.ForceMapping {
				originalAlias = oauthModelAliasForceMappingResponseModel(alias)
			}
			return OAuthModelAliasResult{
				UpstreamModel: preserveResolvedModelSuffix(original, requestResult),
				ForceMapping:  entry.ForceMapping,
				OriginalAlias: originalAlias,
			}
		}
	}
	return OAuthModelAliasResult{}
}

func (m *Manager) applyOAuthModelAliasWithResult(auth *Auth, requestedModel string) OAuthModelAliasResult {
	result := m.resolveOAuthModelAliasWithResult(auth, requestedModel)
	if result.UpstreamModel == "" {
		return OAuthModelAliasResult{UpstreamModel: requestedModel}
	}
	return result
}

func resolveUpstreamModelFromAliasTable(m *Manager, auth *Auth, requestedModel, channel string) OAuthModelAliasResult {
	if m == nil || auth == nil {
		return OAuthModelAliasResult{}
	}
	if channel == "" {
		return OAuthModelAliasResult{}
	}

	requestResult, candidates := modelAliasLookupCandidates(requestedModel)
	baseModel := requestResult.ModelName

	raw := m.oauthModelAlias.Load()
	table, _ := raw.(*oauthModelAliasTable)
	if table == nil || table.reverse == nil {
		return OAuthModelAliasResult{}
	}
	rev := table.reverse[channel]
	if rev == nil {
		return OAuthModelAliasResult{}
	}

	for _, candidate := range candidates {
		key := strings.ToLower(strings.TrimSpace(candidate))
		if key == "" {
			continue
		}
		entry, exists := rev[key]
		if !exists {
			continue
		}

		targetModel := entry.upstreamModel
		if targetModel == "" {
			continue
		}

		if strings.EqualFold(targetModel, baseModel) {
			if !entry.forceMapping {
				return OAuthModelAliasResult{}
			}
			return OAuthModelAliasResult{
				UpstreamModel: preserveResolvedModelSuffix(targetModel, requestResult),
				ForceMapping:  entry.forceMapping,
				OriginalAlias: oauthModelAliasForceMappingResponseModel(entry.configAlias),
			}
		}

		var upstreamModel string
		if thinking.ParseSuffix(targetModel).HasSuffix {
			upstreamModel = targetModel
		} else if requestResult.HasSuffix && requestResult.RawSuffix != "" {
			upstreamModel = targetModel + "(" + requestResult.RawSuffix + ")"
		} else {
			upstreamModel = targetModel
		}

		originalAlias := requestedModel
		if entry.forceMapping {
			originalAlias = oauthModelAliasForceMappingResponseModel(entry.configAlias)
		}
		return OAuthModelAliasResult{
			UpstreamModel: upstreamModel,
			ForceMapping:  entry.forceMapping,
			OriginalAlias: originalAlias,
		}
	}

	return OAuthModelAliasResult{}
}

func modelAliasChannel(auth *Auth) string {
	if auth == nil {
		return ""
	}
	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	authKind := auth.AuthKind()
	return OAuthModelAliasChannel(provider, authKind)
}

func OAuthModelAliasChannel(provider, authKind string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	authKind = normalizeOAuthModelAliasAuthKind(authKind)
	if authKind == "apikey" {
		return ""
	}
	switch provider {
	case "gemini":
		return ""
	case "vertex":
		return "vertex"
	case "claude":
		return "claude"
	case "codex":
		return "codex"
	case "aistudio", "antigravity", "kimi", "kimi-ai", "kimi.ai", "kimi.com", "xai", "meta":
		return provider
	default:
		return provider
	}
}

func normalizeOAuthModelAliasAuthKind(authKind string) string {
	authKind = strings.ToLower(strings.TrimSpace(authKind))
	switch authKind {
	case "api_key", "api-key":
		return "apikey"
	default:
		return authKind
	}
}
