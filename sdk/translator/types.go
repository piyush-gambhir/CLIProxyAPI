package translator

import "context"

type RequestTransform func(model string, rawJSON []byte, stream bool) ([]byte, error)

type RequestEnvelopeTransform func(ctx context.Context, req RequestEnvelope) RequestEnvelope

type ResponseStreamTransform func(ctx context.Context, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte

type ResponseNonStreamTransform func(ctx context.Context, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte

type ResponseTokenCountTransform func(ctx context.Context, count int64) []byte

type ResponseTransform struct {
	Stream ResponseStreamTransform

	NonStream ResponseNonStreamTransform

	TokenCount ResponseTokenCountTransform
}
