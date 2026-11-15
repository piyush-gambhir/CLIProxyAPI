package common

import (
	"github.com/MoeForge/moego/ojson"
)

func DefaultSafetySettings() []map[string]string {
	return []map[string]string{
		{
			"category":  "HARM_CATEGORY_HARASSMENT",
			"threshold": "OFF",
		},
		{
			"category":  "HARM_CATEGORY_HATE_SPEECH",
			"threshold": "OFF",
		},
		{
			"category":  "HARM_CATEGORY_SEXUALLY_EXPLICIT",
			"threshold": "OFF",
		},
		{
			"category":  "HARM_CATEGORY_DANGEROUS_CONTENT",
			"threshold": "OFF",
		},
		{
			"category":  "HARM_CATEGORY_CIVIC_INTEGRITY",
			"threshold": "BLOCK_NONE",
		},
	}
}

func AttachDefaultSafetySettings(rawJSON []byte, path string) []byte {
	if ojson.GetBytesNoCopy(rawJSON, path).Exists() {
		return rawJSON
	}

	out, err := ojson.SetBytes(rawJSON, path, DefaultSafetySettings())
	if err != nil {
		return rawJSON
	}

	return out
}

func AttachDefaultSafetySettingsDoc(doc *ojson.Doc, path string) {
	if doc == nil || doc.Get(path).Exists() {
		return
	}
	_ = doc.Set(path, DefaultSafetySettings())
}
