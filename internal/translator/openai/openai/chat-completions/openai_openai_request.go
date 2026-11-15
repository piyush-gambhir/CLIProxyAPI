package chat_completions

import (
	"github.com/MoeForge/moego/ojson"
)

func ConvertOpenAIRequestToOpenAI(modelName string, inputRawJSON []byte, _ bool) ([]byte, error) {
	currentModel := ojson.GetBytesNoCopy(inputRawJSON, "model")
	if currentModel.Type == ojson.String && currentModel.String() == modelName {
		return inputRawJSON, nil
	}

	updatedJSON, err := ojson.SetBytes(inputRawJSON, "model", modelName)
	if err != nil {

		return inputRawJSON, nil
	}
	return updatedJSON, nil
}
