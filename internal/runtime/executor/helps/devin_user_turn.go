package helps

import (
	"strings"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
)

func CheckDevinUserTurns(prompts []DevinPrompt) ([]DevinPrompt, error) {
	var run translatorcommon.UserRun
	for _, prompt := range prompts {
		switch {
		case prompt.Source == 2:
			run.End()
		case prompt.Source == 1 && !prompt.IsOrphanedTool:
			if prompt.DroppedPart != "" {
				run.Drop(prompt.DroppedPart)
			}
			if devinPromptHasContent(prompt) {
				run.Add()
			}
		default:

			run.Add()
		}
	}
	run.End()
	if errRun := run.Err(); errRun != nil {
		return nil, errRun
	}

	kept := make([]DevinPrompt, 0, len(prompts))
	for _, prompt := range prompts {
		if prompt.Source == 1 && !prompt.IsOrphanedTool && prompt.DroppedPart != "" && !devinPromptHasContent(prompt) {
			continue
		}
		kept = append(kept, prompt)
	}
	if len(kept) == len(prompts) {
		return prompts, nil
	}
	return kept, nil
}

func devinPromptHasContent(prompt DevinPrompt) bool {
	return strings.TrimSpace(prompt.Content) != "" || len(prompt.Images) > 0
}
