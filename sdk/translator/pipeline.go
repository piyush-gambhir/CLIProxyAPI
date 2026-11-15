package translator

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

type RequestEnvelope struct {
	Format    Format
	Model     string
	Stream    bool
	Body      []byte
	ModelInfo *registry.ModelInfo

	ConfigurationUpdatesChanged bool

	Err error
}

type ResponseEnvelope struct {
	Format Format
	Model  string
	Stream bool
	Body   []byte
	Chunks [][]byte
}

type RequestMiddleware func(ctx context.Context, req RequestEnvelope, next RequestHandler) (RequestEnvelope, error)

type ResponseMiddleware func(ctx context.Context, resp ResponseEnvelope, next ResponseHandler) (ResponseEnvelope, error)

type RequestHandler func(ctx context.Context, req RequestEnvelope) (RequestEnvelope, error)

type ResponseHandler func(ctx context.Context, resp ResponseEnvelope) (ResponseEnvelope, error)

type Pipeline struct {
	registry           *Registry
	requestMiddleware  []RequestMiddleware
	responseMiddleware []ResponseMiddleware
}

func NewPipeline(registry *Registry) *Pipeline {
	if registry == nil {
		registry = Default()
	}
	return &Pipeline{registry: registry}
}

func (p *Pipeline) UseRequest(mw RequestMiddleware) {
	if mw != nil {
		p.requestMiddleware = append(p.requestMiddleware, mw)
	}
}

func (p *Pipeline) UseResponse(mw ResponseMiddleware) {
	if mw != nil {
		p.responseMiddleware = append(p.responseMiddleware, mw)
	}
}

func (p *Pipeline) TranslateRequest(ctx context.Context, from, to Format, req RequestEnvelope) (RequestEnvelope, error) {
	terminal := func(ctx context.Context, input RequestEnvelope) (RequestEnvelope, error) {
		return p.registry.TranslateRequestEnvelope(ctx, from, to, input), nil
	}

	handler := terminal
	for i := len(p.requestMiddleware) - 1; i >= 0; i-- {
		mw := p.requestMiddleware[i]
		next := handler
		handler = func(ctx context.Context, r RequestEnvelope) (RequestEnvelope, error) {
			return mw(ctx, r, next)
		}
	}

	return handler(ctx, req)
}

func (p *Pipeline) TranslateResponse(ctx context.Context, from, to Format, resp ResponseEnvelope, originalReq, translatedReq []byte, param *any) (ResponseEnvelope, error) {
	terminal := func(ctx context.Context, input ResponseEnvelope) (ResponseEnvelope, error) {
		if input.Stream {
			input.Chunks = p.registry.TranslateStream(ctx, from, to, input.Model, originalReq, translatedReq, input.Body, param)
		} else {
			input.Body = p.registry.TranslateNonStream(ctx, from, to, input.Model, originalReq, translatedReq, input.Body, param)
		}
		input.Format = to
		return input, nil
	}

	handler := terminal
	for i := len(p.responseMiddleware) - 1; i >= 0; i-- {
		mw := p.responseMiddleware[i]
		next := handler
		handler = func(ctx context.Context, r ResponseEnvelope) (ResponseEnvelope, error) {
			return mw(ctx, r, next)
		}
	}

	return handler(ctx, resp)
}
