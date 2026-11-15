package auth

import (
	"reflect"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
)



func IsAuthTokenPayloadKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "access_token", "refresh_token", "id_token", "session_id",
		"expired", "last_refresh", "expires_in", "timestamp",
		"token_type", "user_code", "verification_uri", "verification_uri_complete":
		return true
	default:
		return false
	}
}



func MergeExistingAuthMetadata(target *Auth, existingMap map[string]any) {
	if target == nil || len(existingMap) == 0 {
		return
	}
	if target.Metadata == nil {
		target.Metadata = make(map[string]any)
	}
	if _, explicitlySet := target.Metadata["disabled"]; !explicitlySet {
		if disabled, ok := existingMap["disabled"].(bool); ok {
			target.Disabled = disabled
		}
	}
	_, targetHadOAuthProvider := target.Metadata["oauth_provider"]
	_, targetHadPortal := target.Metadata["portal"]
	for k, v := range existingMap {
		if IsAuthTokenPayloadKey(k) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(target.Provider), "meta") {
			switch CanonicalCredentialMetadataKey(k) {
			case "api_key", "dca_token", "dca_expired", "dca_expires_at":
				continue
			}
		}
		if strings.EqualFold(strings.TrimSpace(k), "portal") && targetHadOAuthProvider {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(k), "oauth_provider") && targetHadPortal {
			continue
		}
		if _, exists := target.Metadata[k]; !exists {
			target.Metadata[k] = v
		}
	}
	if setter, ok := target.Storage.(interface{ SetMetadata(map[string]any) }); ok {
		setter.SetMetadata(target.Metadata)
	}
}



func MergePreparedAuth(base, current, updated *Auth) *Auth {
	return mergeAuthContent(base, current, updated)
}




func MergeRefreshedAuth(base, current, updated *Auth) *Auth {
	merged := mergeAuthContent(base, current, updated)
	if merged == nil || current == nil || updated == nil {
		return merged
	}
	if base != nil && current.RegistrationEpoch != base.RegistrationEpoch {
		return merged
	}

	merged.CodexRefreshPatch = updated.CodexRefreshPatch


	applyPreservedRejectedTokens(current, merged, true)
	if !updated.LastRefreshedAt.IsZero() {
		merged.LastRefreshedAt = updated.LastRefreshedAt
	}
	if !updated.NextRefreshAfter.IsZero() || (base != nil && !base.NextRefreshAfter.IsZero()) {
		merged.NextRefreshAfter = updated.NextRefreshAfter
	}


	baseErrMsg := ""
	if base != nil && base.LastError != nil {
		baseErrMsg = base.LastError.Message
	}
	currentErrMsg := ""
	if current.LastError != nil {
		currentErrMsg = current.LastError.Message
	}
	hasNewConcurrentError := currentErrMsg != "" && currentErrMsg != baseErrMsg


	baseDisabled := base != nil && (base.Disabled || base.Status == StatusDisabled)
	currentDisabled := current.Disabled || current.Status == StatusDisabled
	updatedDisabled := updated.Disabled || updated.Status == StatusDisabled

	disabledChangedByExecutor := updatedDisabled != baseDisabled
	disabledChangedByUser := currentDisabled != baseDisabled

	finalDisabled := currentDisabled
	if disabledChangedByExecutor && !disabledChangedByUser {
		finalDisabled = updatedDisabled
	}

	if finalDisabled {
		merged.Disabled = true
		merged.Status = StatusDisabled
		merged.Metadata["disabled"] = true
	} else {
		merged.Disabled = false
		if merged.Status == StatusDisabled {
			merged.Status = StatusActive
		}
		merged.Metadata["disabled"] = false

		if hasNewConcurrentError {

			merged.LastError = current.LastError
			merged.Status = current.Status
			merged.Unavailable = current.Unavailable
			merged.StatusMessage = current.StatusMessage
		} else if current.Quota.Exceeded && current.Quota.Reason == "credential_quota" && current.Quota.NextRecoverAt.After(time.Now()) && !devinQuotaObservationRestored(merged, current, time.Now()) {

			merged.Unavailable = current.Unavailable
			merged.Status = current.Status
			merged.StatusMessage = current.StatusMessage
		} else if current.Unavailable && current.NextRetryAfter.After(time.Now()) {

			merged.Unavailable = current.Unavailable
			merged.Status = current.Status
			merged.StatusMessage = current.StatusMessage
		} else if (updated.Status == StatusActive || updated.Status == "") && (!merged.RequiresSignIn || merged.HasValidAccessToken(time.Now())) && (merged.RejectedAccessToken == "" || authAccessToken(merged) != merged.RejectedAccessToken) {

			merged.Status = StatusActive
			merged.Unavailable = false
			merged.StatusMessage = ""
			merged.LastError = nil
		}
	}







	merged.Quota = mergeQuotaObservation(merged.Quota, updated.Quota)




	for model, mergedState := range merged.ModelStates {
		if mergedState == nil {
			continue
		}
		if currentState := current.ModelStates[model]; currentState != nil {
			if currentState.credentialQuotaShadow != nil && mergedState.credentialQuotaShadow == nil {
				mergedState.credentialQuotaShadow = currentState.credentialQuotaShadow.cloneShallow()
			}
			mergedState.credentialQuotaPropagated = mergedState.credentialQuotaPropagated || currentState.credentialQuotaPropagated
		}
	}
	var baseModels map[string]*ModelState
	if base != nil {
		baseModels = base.ModelStates
	}
	if updated.ModelStates != nil {
		if merged.ModelStates == nil {
			merged.ModelStates = make(map[string]*ModelState)
		}
		for model, updState := range updated.ModelStates {
			baseState := baseModels[model]
			currentState := current.ModelStates[model]

			changedByExecutor := !reflect.DeepEqual(baseState, updState)
			changedByUser := !reflect.DeepEqual(baseState, currentState)

			if changedByExecutor && !changedByUser {
				merged.ModelStates[model] = updState
			}
		}
		if baseModels != nil {
			for model, baseState := range baseModels {
				if _, inUpdated := updated.ModelStates[model]; !inUpdated {
					if currentState, ok := current.ModelStates[model]; ok {
						if reflect.DeepEqual(baseState, currentState) {
							delete(merged.ModelStates, model)
						}
					}
				}
			}
		}
	}

	return merged
}

func mergeAuthContent(base, current, updated *Auth) *Auth {
	if current == nil {
		if updated != nil {
			return updated.Clone()
		}
		if base != nil {
			return base.Clone()
		}
		return nil
	}
	if updated == nil {
		return current.Clone()
	}
	if base != nil && current.RegistrationEpoch != base.RegistrationEpoch {

		return current.Clone()
	}

	merged := current.Clone()
	if merged.Metadata == nil {
		merged.Metadata = make(map[string]any)
	}

	var baseMeta map[string]any
	if base != nil {
		baseMeta = base.Metadata
	}


	if updated.Metadata != nil {
		for k, v := range updated.Metadata {
			if strings.EqualFold(strings.TrimSpace(k), "proxy_url") {
				continue
			}
			if updated.CodexRefreshPatch != nil && codex.IsCodexCredentialKey(strings.ToLower(strings.TrimSpace(k))) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(k), "siwc") {
				siwcUpd, okUpd := v.(map[string]any)
				if !okUpd || len(siwcUpd) == 0 {
					continue
				}
				baseVal, hadInBase := baseMeta[k]
				changedByExecutor := !hadInBase || !reflect.DeepEqual(baseVal, v)
				if changedByExecutor {
					mergedSIWC := make(map[string]any)
					if curSIWC, okCur := current.Metadata[k].(map[string]any); okCur {
						for sk, sv := range curSIWC {
							mergedSIWC[sk] = sv
						}
					} else if bSIWC, okBase := baseVal.(map[string]any); okBase {
						for sk, sv := range bSIWC {
							mergedSIWC[sk] = sv
						}
					}
					for sk, sv := range siwcUpd {
						mergedSIWC[sk] = sv
					}
					merged.Metadata[k] = mergedSIWC
				}
				continue
			}
			baseVal, hadInBase := baseMeta[k]
			currentVal, hadInCurrent := current.Metadata[k]

			changedByExecutor := !hadInBase || !reflect.DeepEqual(baseVal, v)
			changedByUser := hadInBase != hadInCurrent || (hadInBase && !reflect.DeepEqual(baseVal, currentVal))

			if changedByExecutor {

				if !changedByUser || IsAuthTokenPayloadKey(k) {
					merged.Metadata[k] = v
				}
			}
		}

		if baseMeta != nil {
			for k, baseVal := range baseMeta {
				trimmedKey := strings.ToLower(strings.TrimSpace(k))
				if trimmedKey == "proxy_url" || trimmedKey == "siwc" || trimmedKey == "use_siwc" {
					continue
				}
				if _, inUpdated := updated.Metadata[k]; !inUpdated {
					if currentVal, ok := current.Metadata[k]; ok {
						if reflect.DeepEqual(baseVal, currentVal) {
							delete(merged.Metadata, k)
						}
					}
				}
			}
		}
	}

	if updated.CodexRefreshPatch != nil {
		merged.Metadata = updated.CodexRefreshPatch.Apply(merged.Metadata)
	}


	if updated.Storage != nil {
		merged.Storage = updated.Storage
	}
	if updated.Runtime != nil {
		merged.Runtime = updated.Runtime
	}


	baseStruct := ""
	if base != nil {
		baseStruct = strings.TrimSpace(base.ProxyURL)
	}
	currentStruct := strings.TrimSpace(current.ProxyURL)
	updatedStruct := strings.TrimSpace(updated.ProxyURL)

	baseMetaProxy := ""
	if base != nil && base.Metadata != nil {
		if s, ok := base.Metadata["proxy_url"].(string); ok {
			baseMetaProxy = strings.TrimSpace(s)
		}
	}
	currentMetaProxy := ""
	if current.Metadata != nil {
		if s, ok := current.Metadata["proxy_url"].(string); ok {
			currentMetaProxy = strings.TrimSpace(s)
		}
	}
	updatedMetaProxy := ""
	if updated.Metadata != nil {
		if s, ok := updated.Metadata["proxy_url"].(string); ok {
			updatedMetaProxy = strings.TrimSpace(s)
		}
	}

	userChangedStruct := currentStruct != baseStruct
	userChangedMeta := currentMetaProxy != baseMetaProxy
	execChangedStruct := updatedStruct != baseStruct
	execChangedMeta := updatedMetaProxy != baseMetaProxy

	finalProxy := currentStruct
	if currentMetaProxy != "" && currentStruct == "" && !userChangedStruct {
		finalProxy = currentMetaProxy
	}

	if userChangedStruct || userChangedMeta {

		if userChangedStruct && !userChangedMeta {
			finalProxy = currentStruct
		} else if userChangedMeta && !userChangedStruct {
			finalProxy = currentMetaProxy
		} else if currentStruct != "" {
			finalProxy = currentStruct
		} else {
			finalProxy = currentMetaProxy
		}
	} else if execChangedStruct || execChangedMeta {

		if execChangedStruct && !execChangedMeta {
			finalProxy = updatedStruct
		} else if execChangedMeta && !execChangedStruct {
			finalProxy = updatedMetaProxy
		} else if updatedStruct != "" {
			finalProxy = updatedStruct
		} else {
			finalProxy = updatedMetaProxy
		}
	}

	if finalProxy != "" {
		merged.ProxyURL = finalProxy
		merged.Metadata["proxy_url"] = finalProxy
	} else {
		merged.ProxyURL = ""
		delete(merged.Metadata, "proxy_url")
	}


	basePrefix := ""
	if base != nil {
		basePrefix = strings.TrimSpace(base.Prefix)
	}
	currentPrefix := strings.TrimSpace(current.Prefix)
	updatedPrefix := strings.TrimSpace(updated.Prefix)

	if updatedPrefix != basePrefix && currentPrefix == basePrefix {
		merged.Prefix = updatedPrefix
	} else {
		merged.Prefix = currentPrefix
	}


	if updated.Attributes != nil {
		if merged.Attributes == nil {
			merged.Attributes = make(map[string]string)
		}
		var baseAttrs map[string]string
		if base != nil {
			baseAttrs = base.Attributes
		}
		for k, v := range updated.Attributes {
			baseVal, hadInBase := baseAttrs[k]
			currentVal, hadInCurrent := current.Attributes[k]

			changedByExecutor := !hadInBase || baseVal != v
			changedByUser := hadInBase != hadInCurrent || (hadInBase && baseVal != currentVal)

			if changedByExecutor && !changedByUser {
				merged.Attributes[k] = v
			}
		}
		if baseAttrs != nil {
			for k, baseVal := range baseAttrs {
				if _, inUpdated := updated.Attributes[k]; !inUpdated {
					if currentVal, ok := current.Attributes[k]; ok {
						if baseVal == currentVal {
							delete(merged.Attributes, k)
						}
					}
				}
			}
		}
	}

	return merged
}
