package common

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
	sigcompat "github.com/router-for-me/CLIProxyAPI/v8/internal/signature"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
)

func ResolveCodexReplayTargetModel(modelName string, rawPayload []byte) string {
	if sigcompat.SignatureProviderFromModelName(thinking.ParseSuffix(modelName).ModelName) != sigcompat.SignatureProviderUnknown {
		return modelName
	}
	if len(rawPayload) > 0 {
		if rawModel := strings.TrimSpace(ojson.GetBytesNoCopy(rawPayload, "model").String()); rawModel != "" {
			switch sigcompat.SignatureProviderFromModelName(thinking.ParseSuffix(rawModel).ModelName) {
			case sigcompat.SignatureProviderMeta, sigcompat.SignatureProviderGrok, sigcompat.SignatureProviderGPT:
				return strings.Clone(rawModel)
			}
		}
	}
	return modelName
}

func CodexReplaySignatureForTarget(modelName, rawSignature string) (string, bool) {
	rawSignature = strings.TrimSpace(rawSignature)
	if rawSignature == "" {
		return "", false
	}
	target := sigcompat.SignatureProviderFromModelName(thinking.ParseSuffix(modelName).ModelName)
	detected := sigcompat.DetectSignatureProvider(rawSignature)
	if detected != sigcompat.SignatureProviderUnknown {
		effectiveTarget := target
		if effectiveTarget == sigcompat.SignatureProviderUnknown {
			effectiveTarget = sigcompat.SignatureProviderGPT
		}
		if detected != effectiveTarget {
			return "", false
		}
		normalized, ok := sigcompat.CompatibleSignatureForProvider(effectiveTarget, rawSignature)
		return normalized, ok && normalized != ""
	}
	if target == sigcompat.SignatureProviderGrok {
		if _, err := sigcompat.InspectGrokEncryptedContent(rawSignature); err == nil {
			return rawSignature, true
		}
	}
	return "", false
}
