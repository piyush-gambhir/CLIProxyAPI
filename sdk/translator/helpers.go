package translator

import "context"


func TranslateRequestByFormatName(from, to Format, model string, rawJSON []byte, stream bool) []byte {
	return TranslateRequest(from, to, model, rawJSON, stream)
}


func HasRequestTransformerByFormatName(from, to Format) bool {
	return HasRequestTransformer(from, to)
}


func HasResponseTransformerByFormatName(from, to Format) bool {
	return HasResponseTransformer(from, to)
}


func HasStreamResponseTransformerByFormatName(from, to Format) bool {
	return HasStreamResponseTransformer(from, to)
}


func HasNonStreamResponseTransformerByFormatName(from, to Format) bool {
	return HasNonStreamResponseTransformer(from, to)
}


func TranslateStreamByFormatName(ctx context.Context, from, to Format, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	return TranslateStream(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, rawJSON, param)
}


func TranslateNonStreamByFormatName(ctx context.Context, from, to Format, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	return TranslateNonStream(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, rawJSON, param)
}


func TranslateTokenCountByFormatName(ctx context.Context, from, to Format, count int64, rawJSON []byte) []byte {
	return TranslateTokenCount(ctx, from, to, count, rawJSON)
}
