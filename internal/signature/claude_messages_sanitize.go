package signature

import (
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

type ClaudeMessagesSignatureSanitizeOptions struct {
	TargetProvider                SignatureProvider
	TargetModel                   string
	DropEmptyMessages             bool
	DropToolSignatures            bool
	DropEmptyThinkingPlaceholders bool

	PreserveEmptyThinkingBlocks bool
}

const CodexCommentarySignatureMarker = "cpa:commentary:v1"

func IsCodexCommentarySignatureMarker(rawSignature string) bool {
	return strings.TrimSpace(rawSignature) == CodexCommentarySignatureMarker
}

func RestoreCodexCommentaryMarkers(payload []byte) ([]byte, int) {
	messages := ojson.GetBytesNoCopy(payload, "messages")
	if !messages.IsArray() {
		return payload, 0
	}
	doc := ojson.NewDoc(payload)
	restored := RestoreCodexCommentaryMarkersDoc(doc)
	if restored == 0 {
		return payload, 0
	}
	return doc.Bytes(), restored
}

func RestoreCodexCommentaryMarkersDoc(doc *ojson.Doc) int {
	if doc == nil {
		return 0
	}
	messages := doc.Get("messages")
	if !messages.IsArray() {
		return 0
	}
	messageResults := messages.Array()
	restored := 0
	var droppedMessages []int
	for i, message := range messageResults {
		content := message.Get("content")
		if !content.IsArray() {
			continue
		}
		contentResults := content.Array()
		var droppedParts []int
		for j, part := range contentResults {
			if part.Get("type").String() != "thinking" || !IsCodexCommentarySignatureMarker(part.Get("signature").String()) {
				continue
			}
			restored++
			partPath := fmt.Sprintf("messages.%d.content.%d", i, j)
			if text := part.Get("thinking").String(); text != "" {
				_ = doc.Set(partPath+".type", "text")
				_ = doc.Delete(partPath + ".thinking")
				_ = doc.Delete(partPath + ".signature")
				_ = doc.Set(partPath+".text", text)
			} else {
				droppedParts = append(droppedParts, j)
			}
		}
		if len(droppedParts) == 0 {
			continue
		}
		if len(droppedParts) == len(contentResults) {
			droppedMessages = append(droppedMessages, i)
			continue
		}
		for k := len(droppedParts) - 1; k >= 0; k-- {
			_ = doc.Delete(fmt.Sprintf("messages.%d.content.%d", i, droppedParts[k]))
		}
	}
	for k := len(droppedMessages) - 1; k >= 0; k-- {
		_ = doc.Delete(fmt.Sprintf("messages.%d", droppedMessages[k]))
	}
	return restored
}

type SignatureSanitizeReport struct {
	TargetProvider     SignatureProvider
	Preserved          int
	DroppedBlocks      int
	DroppedSignatures  int
	ReplacedSignatures int
	Decisions          []SignatureCompatibilityDecision
}

func SanitizeClaudeMessagesSignaturesForModel(payload []byte, targetModel string) ([]byte, SignatureSanitizeReport) {
	return SanitizeClaudeMessagesSignaturesForTarget(payload, ClaudeMessagesSignatureSanitizeOptions{
		TargetProvider:    SignatureProviderFromModelName(targetModel),
		TargetModel:       targetModel,
		DropEmptyMessages: true,
	})
}

func SanitizeClaudeMessagesForClaudeUpstream(payload []byte, targetModel string, preserveEmptyThinkingBlocks ...bool) ([]byte, SignatureSanitizeReport) {
	preserveEmpty := len(preserveEmptyThinkingBlocks) > 0 && preserveEmptyThinkingBlocks[0]
	return SanitizeClaudeMessagesSignaturesForTarget(payload, ClaudeMessagesSignatureSanitizeOptions{
		TargetProvider:                SignatureProviderClaude,
		TargetModel:                   targetModel,
		DropEmptyMessages:             true,
		DropToolSignatures:            true,
		DropEmptyThinkingPlaceholders: !preserveEmpty,
		PreserveEmptyThinkingBlocks:   preserveEmpty,
	})
}

func SanitizeClaudeMessagesSignaturesForTarget(payload []byte, opts ClaudeMessagesSignatureSanitizeOptions) ([]byte, SignatureSanitizeReport) {
	targetProvider := normalizeSignatureTargetProvider(opts.TargetProvider)
	if targetProvider == SignatureProviderUnknown && opts.TargetModel != "" {
		targetProvider = SignatureProviderFromModelName(opts.TargetModel)
	}
	report := SignatureSanitizeReport{TargetProvider: targetProvider}

	messages := ojson.GetBytesNoCopy(payload, "messages")
	if !messages.IsArray() {
		return payload, report
	}

	messageResults := messages.Array()
	doc := ojson.NewDoc(payload)
	modified := false
	var droppedMessages []int

	for i, message := range messageResults {
		content := message.Get("content")
		if !content.IsArray() {
			continue
		}

		contentResults := content.Array()
		var droppedParts []int

		for j, part := range contentResults {
			partPath := fmt.Sprintf("messages.%d.content.%d", i, j)
			partType := part.Get("type").String()
			if partType == "tool_use" {
				if opts.DropToolSignatures {
					if stripClaudeToolUseSignatureFieldsDoc(doc, partPath) {
						modified = true
						report.DroppedSignatures++
					}
					continue
				}
				changed, decisions := sanitizeClaudeToolUseSignatureDoc(doc, part, partPath, targetProvider, opts.TargetModel, i, j)
				report.Decisions = append(report.Decisions, decisions...)
				if changed {
					modified = true
				}
				for _, decision := range decisions {
					switch decision.Action {
					case SignatureActionPreserve:
						report.Preserved++
					case SignatureActionReplaceWithGeminiBypass:
						report.ReplacedSignatures++
					default:
						report.DroppedSignatures++
					}
				}
				continue
			}

			if partType != "thinking" {
				continue
			}

			rawSignature := part.Get("signature").String()
			if IsCodexCommentarySignatureMarker(rawSignature) {

				if text := part.Get("thinking").String(); text != "" {
					_ = doc.Set(partPath+".type", "text")
					_ = doc.Delete(partPath + ".thinking")
					_ = doc.Delete(partPath + ".signature")
					_ = doc.Set(partPath+".text", text)
					report.DroppedSignatures++
					modified = true
					continue
				}
				report.DroppedBlocks++
				droppedParts = append(droppedParts, j)
				modified = true
				continue
			}
			if opts.PreserveEmptyThinkingBlocks {
				report.Preserved++
				continue
			}
			if targetProvider == SignatureProviderClaude && isEmptyClaudeThinkingPlaceholder(part) && !opts.DropEmptyThinkingPlaceholders {
				continue
			}

			decision := DecideSignatureCompatibilityForModel(targetProvider, opts.TargetModel, rawSignature, SignatureBlockKindClaudeThinking)
			decision.Reason = fmt.Sprintf("messages[%d].content[%d]: %s", i, j, decision.Reason)
			report.Decisions = append(report.Decisions, decision)

			switch decision.Action {
			case SignatureActionPreserve:
				report.Preserved++
				if decision.NormalizedSignature != "" && decision.NormalizedSignature != rawSignature {
					_ = doc.Set(partPath+".signature", decision.NormalizedSignature)
					modified = true
				}
			case SignatureActionReplaceWithGeminiBypass:
				report.ReplacedSignatures++
				_ = doc.Set(partPath+".signature", decision.ReplacementSignature)
				modified = true
			case SignatureActionDropSignature:
				report.DroppedSignatures++
				_ = doc.Delete(partPath + ".signature")
				modified = true
			default:
				report.DroppedBlocks++
				droppedParts = append(droppedParts, j)
				modified = true
			}
		}

		if len(droppedParts) > 0 {
			if len(droppedParts) == len(contentResults) && opts.DropEmptyMessages {
				droppedMessages = append(droppedMessages, i)
				continue
			}
			for k := len(droppedParts) - 1; k >= 0; k-- {
				_ = doc.Delete(fmt.Sprintf("messages.%d.content.%d", i, droppedParts[k]))
			}
		}
	}

	for k := len(droppedMessages) - 1; k >= 0; k-- {
		_ = doc.Delete(fmt.Sprintf("messages.%d", droppedMessages[k]))
	}

	if !modified {
		return payload, report
	}
	return doc.Bytes(), report
}

func stripClaudeToolUseSignatureFieldsDoc(doc *ojson.Doc, partPath string) bool {
	changed := false
	for _, relPath := range claudeToolUseProvenancePaths() {
		sigPath := partPath + "." + relPath
		if !doc.Get(sigPath).Exists() {
			continue
		}
		if doc.Delete(sigPath) == nil {
			changed = true
		}
	}
	if deleteEmptyJSONObjectPathDoc(doc, partPath+".extra_content.google") {
		changed = true
	}
	if deleteEmptyJSONObjectPathDoc(doc, partPath+".extra_content") {
		changed = true
	}
	return changed
}

func sanitizeClaudeToolUseSignatureDoc(doc *ojson.Doc, part ojson.Result, partPath string, targetProvider SignatureProvider, targetModel string, messageIdx, partIdx int) (bool, []SignatureCompatibilityDecision) {
	changed := false
	var decisions []SignatureCompatibilityDecision

	for _, relPath := range claudeToolUseSignaturePaths() {
		sigResult := part.Get(relPath)
		if !sigResult.Exists() {
			continue
		}
		sigPath := partPath + "." + relPath

		blockKind := SignatureBlockKindGeminiFunctionCall
		if targetProvider == SignatureProviderClaude {
			blockKind = SignatureBlockKindClaudeThinking
		} else if targetProvider == SignatureProviderGPT {
			blockKind = SignatureBlockKindGPTReasoning
		}
		decision := DecideSignatureCompatibilityForModel(targetProvider, targetModel, sigResult.String(), blockKind)
		decision.Reason = fmt.Sprintf("messages[%d].content[%d].%s: %s", messageIdx, partIdx, relPath, decision.Reason)
		decisions = append(decisions, decision)

		switch decision.Action {
		case SignatureActionPreserve:
			if decision.NormalizedSignature != "" && decision.NormalizedSignature != sigResult.String() {
				if doc.Set(sigPath, decision.NormalizedSignature) == nil {
					changed = true
				}
			}
		case SignatureActionReplaceWithGeminiBypass:
			if doc.Set(sigPath, decision.ReplacementSignature) == nil {
				changed = true
			}
		default:
			if doc.Delete(sigPath) == nil {
				changed = true
			}
		}
	}

	if deleteEmptyJSONObjectPathDoc(doc, partPath+".extra_content.google") {
		changed = true
	}
	if deleteEmptyJSONObjectPathDoc(doc, partPath+".extra_content") {
		changed = true
	}

	return changed, decisions
}

func deleteEmptyJSONObjectPathDoc(doc *ojson.Doc, path string) bool {
	result := doc.Get(path)
	if !result.Exists() || !result.IsObject() || len(result.Map()) != 0 {
		return false
	}
	return doc.Delete(path) == nil
}

func claudeToolUseSignaturePaths() []string {
	return []string{
		"signature",
		"thoughtSignature",
		"thought_signature",
		"extra_content.google.thought_signature",
	}
}

func claudeToolUseProvenancePaths() []string {
	return append(claudeToolUseSignaturePaths(), "model")
}
