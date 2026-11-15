package helps

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
)

const (
	CodexEmptyIncompleteStreamMessage = "stream error: upstream terminated with incomplete empty response (0 tokens)"
)

func HasMeaningfulCodexOutputDelta(eventData []byte) bool {
	eventType := ojson.GetBytesNoCopy(eventData, "type").String()
	switch eventType {
	case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		delta := ojson.GetBytesNoCopy(eventData, "delta")
		return delta.Exists() && len(strings.TrimSpace(delta.String())) > 0
	case "response.function_call_arguments.delta":
		delta := ojson.GetBytesNoCopy(eventData, "delta")
		return delta.Exists() && len(strings.TrimSpace(delta.String())) > 0
	}
	return false
}

func IsCodexTerminalEmptyIncomplete(eventData []byte, outputItemsCount int, sawOutputDelta bool) bool {
	eventType := ojson.GetBytesNoCopy(eventData, "type").String()
	if eventType != "response.incomplete" {
		return false
	}

	if sawOutputDelta {
		return false
	}

	if outputItemsCount > 0 {
		return false
	}

	output := ojson.GetBytesNoCopy(eventData, "response.output")
	if output.Exists() && output.IsArray() && len(output.Array()) > 0 {
		return false
	}

	outputTokens := ojson.GetBytesNoCopy(eventData, "response.usage.output_tokens")
	if !outputTokens.Exists() || outputTokens.Type != ojson.Number {
		return false
	}
	if outputTokens.Num != 0 || strings.TrimSpace(outputTokens.Raw) != "0" {
		return false
	}
	return true
}
