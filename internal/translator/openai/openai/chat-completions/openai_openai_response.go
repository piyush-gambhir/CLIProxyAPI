

package chat_completions

import (
	"bytes"
	"context"
)













func ConvertOpenAIResponseToOpenAI(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	if param != nil {
		if done, ok := (*param).(bool); ok && done {

			return [][]byte{}
		}
	}
	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[5:])
	}
	if bytes.Equal(rawJSON, []byte("[DONE]")) {
		if param != nil {
			*param = true
		}
		return [][]byte{}
	}
	return [][]byte{rawJSON}
}











func ConvertOpenAIResponseToOpenAINonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	return rawJSON
}
