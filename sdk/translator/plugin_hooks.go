package translator

import "context"


type PluginHooks interface {
	NormalizeRequest(ctx context.Context, from, to Format, model string, body []byte, stream bool) []byte
	TranslateRequest(ctx context.Context, from, to Format, model string, body []byte, stream bool) ([]byte, bool)
	NormalizeResponseBefore(ctx context.Context, from, to Format, model string, originalRequestRawJSON, requestRawJSON, body []byte, stream bool) []byte
	TranslateResponse(ctx context.Context, from, to Format, model string, originalRequestRawJSON, requestRawJSON, body []byte, stream bool) ([]byte, bool)
	NormalizeResponseAfter(ctx context.Context, from, to Format, model string, originalRequestRawJSON, requestRawJSON, body []byte, stream bool) []byte
}
