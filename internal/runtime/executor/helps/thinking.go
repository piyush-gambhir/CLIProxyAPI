package helps

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func ApplyThinkingWithSourcePayload(body, currentSourcePayload, originalSourcePayload []byte, model, fromFormat, toFormat, providerKey string) ([]byte, error) {
	summary := translatedRequestSummaryConfig(body, currentSourcePayload, originalSourcePayload, model, fromFormat, toFormat)
	return thinking.ApplyThinkingWithSummary(body, model, fromFormat, toFormat, providerKey, summary)
}

func ApplyThinkingWithSourcePayloadDoc(doc *ojson.Doc, currentSourcePayload, originalSourcePayload []byte, model, fromFormat, toFormat, providerKey string) error {
	if doc == nil {
		return nil
	}
	summary := translatedRequestSummaryConfigDoc(doc, currentSourcePayload, originalSourcePayload, model, fromFormat, toFormat)
	return thinking.ApplyThinkingWithSummaryDoc(doc, model, fromFormat, toFormat, providerKey, summary)
}

func translatedRequestSummaryConfig(body, currentSourcePayload, originalSourcePayload []byte, model, fromFormat, toFormat string) thinking.SummaryConfig {
	fromFormat = strings.ToLower(strings.TrimSpace(fromFormat))
	toFormat = strings.ToLower(strings.TrimSpace(toFormat))

	var targetSummary thinking.SummaryConfig
	if fromFormat == toFormat {
		targetSummary = thinking.ExtractSummaryConfig(body, toFormat)
	} else {
		targetSummary = thinking.ExtractExplicitSummaryConfig(body, toFormat)
	}
	if targetSummary.Mode != thinking.SummaryUnspecified {
		return targetSummary
	}

	currentSummary := thinking.ExtractTranslatedSummaryConfig(currentSourcePayload, fromFormat, toFormat)
	originalSummary := thinking.ExtractTranslatedSummaryConfig(originalSourcePayload, fromFormat, toFormat)
	if currentSummary.Mode == thinking.SummaryUnspecified {
		return originalSummary
	}

	from := sdktranslator.FromString(fromFormat)
	to := sdktranslator.FromString(toFormat)
	if !sdktranslator.HasRequestTransformer(from, to) {

		return thinking.SummaryConfig{}
	}

	candidate := thinking.ApplySummaryConfigForModel(body, toFormat, model, currentSummary)
	if thinking.ExtractExplicitSummaryConfig(candidate, toFormat).Mode != thinking.SummaryUnspecified {

		return thinking.SummaryConfig{}
	}

	return currentSummary
}

func translatedRequestSummaryConfigDoc(doc *ojson.Doc, currentSourcePayload, originalSourcePayload []byte, model, fromFormat, toFormat string) thinking.SummaryConfig {
	if doc == nil || doc.Len() == 0 {
		return thinking.SummaryConfig{}
	}
	fromFormat = strings.ToLower(strings.TrimSpace(fromFormat))
	toFormat = strings.ToLower(strings.TrimSpace(toFormat))

	var targetSummary thinking.SummaryConfig
	if fromFormat == toFormat {
		targetSummary = thinking.ExtractSummaryConfigDoc(doc, toFormat)
	} else {
		targetSummary = thinking.ExtractExplicitSummaryConfigDoc(doc, toFormat)
	}
	if targetSummary.Mode != thinking.SummaryUnspecified {
		return targetSummary
	}

	currentSummary := thinking.ExtractTranslatedSummaryConfig(currentSourcePayload, fromFormat, toFormat)
	originalSummary := thinking.ExtractTranslatedSummaryConfig(originalSourcePayload, fromFormat, toFormat)
	if currentSummary.Mode == thinking.SummaryUnspecified {
		return originalSummary
	}

	from := sdktranslator.FromString(fromFormat)
	to := sdktranslator.FromString(toFormat)
	if !sdktranslator.HasRequestTransformer(from, to) {
		return thinking.SummaryConfig{}
	}

	if thinking.CanRepresentSummaryConfigForModelDoc(doc, toFormat, model, currentSummary) {
		return thinking.SummaryConfig{}
	}

	return currentSummary
}
