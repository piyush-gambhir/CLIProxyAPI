package translator

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

var registry = sdktranslator.Default()

func Register(from, to string, request interfaces.TranslateRequestFunc, response interfaces.TranslateResponse) {
	registry.Register(sdktranslator.FromString(from), sdktranslator.FromString(to), request, response)
}

func Request(from, to, modelName string, rawJSON []byte, stream bool) []byte {
	return registry.TranslateRequest(sdktranslator.FromString(from), sdktranslator.FromString(to), modelName, rawJSON, stream)
}

func NeedConvert(from, to string) bool {
	return registry.HasResponseTransformer(sdktranslator.FromString(from), sdktranslator.FromString(to))
}

func Response(from, to string, ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	return registry.TranslateStream(ctx, sdktranslator.FromString(from), sdktranslator.FromString(to), modelName, originalRequestRawJSON, requestRawJSON, rawJSON, param)
}

func ResponseNonStream(from, to string, ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	return registry.TranslateNonStream(ctx, sdktranslator.FromString(from), sdktranslator.FromString(to), modelName, originalRequestRawJSON, requestRawJSON, rawJSON, param)
}
