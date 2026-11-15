package helps

import (
	"net/http"

	"github.com/MoeForge/moego/ojson"
	toolschema "github.com/router-for-me/CLIProxyAPI/v8/internal/client/codex/tool-schema"
)

func IsCodexUserAgent(headers http.Header) bool {
	return toolschema.IsCodexUserAgent(headers)
}

func NormalizeCodexToolIntegerTypes(body []byte, headers http.Header) []byte {
	return toolschema.NormalizeCodexToolIntegerTypes(body, headers)
}

func NormalizeCodexToolIntegerTypesDoc(doc *ojson.Doc, headers http.Header) bool {
	return toolschema.NormalizeCodexToolIntegerTypesDoc(doc, headers)
}

func NormalizeCodexToolSchemas(body []byte) []byte {
	return toolschema.NormalizeCodexToolSchemas(body)
}

func NormalizeCodexToolSchemasDoc(doc *ojson.Doc) bool {
	return toolschema.NormalizeCodexToolSchemasDoc(doc)
}
