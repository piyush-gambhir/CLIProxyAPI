package translator

import (
	"context"
	"strings"
	"sync"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	log "github.com/sirupsen/logrus"
)


type Registry struct {
	mu        sync.RWMutex
	requests  map[Format]map[Format]RequestEnvelopeTransform
	responses map[Format]map[Format]ResponseTransform
	hooks     PluginHooks
}


func NewRegistry() *Registry {
	return &Registry{
		requests:  make(map[Format]map[Format]RequestEnvelopeTransform),
		responses: make(map[Format]map[Format]ResponseTransform),
	}
}


func (r *Registry) Register(from, to Format, request RequestTransform, response ResponseTransform) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.requests[from]; !ok {
		r.requests[from] = make(map[Format]RequestEnvelopeTransform)
	}
	if request != nil {
		r.requests[from][to] = func(_ context.Context, req RequestEnvelope) RequestEnvelope {
			req.Body, req.Err = request(req.Model, req.Body, req.Stream)
			return req
		}
	}

	if _, ok := r.responses[from]; !ok {
		r.responses[from] = make(map[Format]ResponseTransform)
	}
	r.responses[from][to] = response
}



func (r *Registry) RegisterRequestEnvelope(from, to Format, request RequestEnvelopeTransform) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.requests[from]; !ok {
		r.requests[from] = make(map[Format]RequestEnvelopeTransform)
	}
	if request != nil {
		r.requests[from][to] = request
	}
}



func (r *Registry) Unregister(from, to Format) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if byTarget, ok := r.requests[from]; ok {
		delete(byTarget, to)
		if len(byTarget) == 0 {
			delete(r.requests, from)
		}
	}
	if byTarget, ok := r.responses[from]; ok {
		delete(byTarget, to)
		if len(byTarget) == 0 {
			delete(r.responses, from)
		}
	}
}


func (r *Registry) SetPluginHooks(hooks PluginHooks) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.hooks = hooks
}


func (r *Registry) HasPluginHooks() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.hooks != nil
}





func (r *Registry) TranslateRequest(from, to Format, model string, rawJSON []byte, stream bool) []byte {
	req := r.TranslateRequestEnvelope(context.Background(), from, to, RequestEnvelope{
		Format: from,
		Model:  model,
		Stream: stream,
		Body:   rawJSON,
	})
	return req.Body
}



func (r *Registry) TranslateRequestEnvelope(ctx context.Context, from, to Format, req RequestEnvelope) RequestEnvelope {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	var fn RequestEnvelopeTransform
	if byTarget, ok := r.requests[from]; ok {
		fn = byTarget[to]
	}
	hooks := r.hooks
	r.mu.RUnlock()

	if fn != nil {
		summaryConfig := thinking.ExtractTranslatedSummaryConfig(req.Body, from.String(), to.String())
		req = fn(ctx, req)
		req.Body = thinking.ApplySummaryConfigForModel(req.Body, to.String(), req.Model, summaryConfig)
		if hooks != nil {


			before := configurationUpdates(req.Body)
			req.Body = hooks.NormalizeRequest(ctx, from, to, req.Model, req.Body, req.Stream)
			req.ConfigurationUpdatesChanged = req.ConfigurationUpdatesChanged || updatesChanged(before, configurationUpdates(req.Body))
		}
		req.Format = to
		return req
	}

	if req.Model != "" && ojson.GetBytesNoCopy(req.Body, "model").String() != req.Model {
		if updated, err := ojson.SetBytes(req.Body, "model", req.Model); err != nil {
			log.Warnf("translator: failed to normalize model in request fallback: %v", err)
		} else {
			req.Body = updated
		}
	}
	if hooks == nil {


		req.Format = to
		return req
	}




	before := configurationUpdates(req.Body)
	req.Body = hooks.NormalizeRequest(ctx, from, to, req.Model, req.Body, req.Stream)
	req.ConfigurationUpdatesChanged = req.ConfigurationUpdatesChanged || updatesChanged(before, configurationUpdates(req.Body))
	summaryConfig := thinking.ExtractTranslatedSummaryConfig(req.Body, from.String(), to.String())
	if translated, ok := hooks.TranslateRequest(ctx, from, to, req.Model, req.Body, req.Stream); ok {
		req.Body = thinking.ApplySummaryConfigForModel(translated, to.String(), req.Model, summaryConfig)
	}
	req.Format = to
	return req
}



func configurationUpdates(body []byte) []string {
	input := ojson.GetBytesNoCopy(body, "input")
	if !input.IsArray() {
		return nil
	}
	var updates []string
	input.ForEach(func(_, item ojson.Result) bool {
		if item.Get("type").String() == "configuration_update" {
			updates = append(updates, strings.Clone(item.Raw))
		}
		return true
	})
	return updates
}

func updatesChanged(before, after []string) bool {
	if len(before) != len(after) {
		return true
	}
	for i, item := range before {
		if item != after[i] {
			return true
		}
	}
	return false
}


func (r *Registry) HasRequestTransformer(from, to Format) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if byTarget, ok := r.requests[from]; ok {
		if fn, isOk := byTarget[to]; isOk && fn != nil {
			return true
		}
	}
	return false
}


func (r *Registry) HasResponseTransformer(from, to Format) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if byTarget, ok := r.responses[from]; ok {
		if fn, isOk := byTarget[to]; isOk && hasAnyResponseTransform(fn) {
			return true
		}
	}
	return false
}


func (r *Registry) HasStreamResponseTransformer(from, to Format) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if byTarget, ok := r.responses[from]; ok {
		if fn, isOk := byTarget[to]; isOk && fn.Stream != nil {
			return true
		}
	}
	return false
}


func (r *Registry) HasNonStreamResponseTransformer(from, to Format) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if byTarget, ok := r.responses[from]; ok {
		if fn, isOk := byTarget[to]; isOk && fn.NonStream != nil {
			return true
		}
	}
	return false
}


func (r *Registry) HasTokenCountTransformer(from, to Format) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if byTarget, ok := r.responses[to]; ok {
		if fn, isOk := byTarget[from]; isOk && fn.TokenCount != nil {
			return true
		}
	}
	return false
}


func (r *Registry) TranslateStream(ctx context.Context, from, to Format, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	r.mu.RLock()
	var stream ResponseStreamTransform
	if byTarget, ok := r.responses[to]; ok {
		stream = byTarget[from].Stream
	}
	hooks := r.hooks
	r.mu.RUnlock()

	body := rawJSON
	if hooks != nil {
		body = hooks.NormalizeResponseBefore(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, body, true)
	}

	var outputs [][]byte
	usedNativeTransform := false
	if stream != nil {
		usedNativeTransform = true
		outputs = stream(ctx, model, originalRequestRawJSON, requestRawJSON, body, param)
	} else if hooks != nil {
		if translated, ok := hooks.TranslateResponse(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, body, true); ok {
			outputs = [][]byte{translated}
		}
	}

	if translationToolInputFailed(param) {
		return outputs
	}
	if outputs == nil && !usedNativeTransform {
		outputs = [][]byte{body}
	}
	if hooks != nil {
		for i, output := range outputs {
			outputs[i] = hooks.NormalizeResponseAfter(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, output, true)
		}
	}
	return outputs
}


func (r *Registry) TranslateNonStream(ctx context.Context, from, to Format, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	r.mu.RLock()
	var fn ResponseTransform
	if byTarget, ok := r.responses[to]; ok {
		fn = byTarget[from]
	}
	hooks := r.hooks
	r.mu.RUnlock()

	body := rawJSON
	if hooks != nil {
		body = hooks.NormalizeResponseBefore(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, body, false)
	}
	if fn.NonStream != nil {
		body = fn.NonStream(ctx, model, originalRequestRawJSON, requestRawJSON, body, param)
	} else if hooks != nil {
		if translated, ok := hooks.TranslateResponse(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, body, false); ok {
			body = translated
		}
	}
	if translationToolInputFailed(param) || (fn.NonStream != nil && body == nil) {
		return nil
	}
	if hooks != nil {
		body = hooks.NormalizeResponseAfter(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, body, false)
	}
	return body
}


func translationToolInputFailed(param *any) bool {
	if param == nil {
		return false
	}
	state, okState := (*param).(interface{ ToolInputError() error })
	return okState && state.ToolInputError() != nil
}


func (r *Registry) TranslateTokenCount(ctx context.Context, from, to Format, count int64, rawJSON []byte) []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if byTarget, ok := r.responses[to]; ok {
		if fn, isOk := byTarget[from]; isOk && fn.TokenCount != nil {
			return fn.TokenCount(ctx, count)
		}
	}
	return rawJSON
}



func (r *Registry) NormalizeRequest(ctx context.Context, from, to Format, model string, body []byte, stream bool) []byte {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	hooks := r.hooks
	r.mu.RUnlock()

	if hooks != nil {
		return hooks.NormalizeRequest(ctx, from, to, model, body, stream)
	}
	return body
}

var defaultRegistry = NewRegistry()


func Default() *Registry {
	return defaultRegistry
}


func Register(from, to Format, request RequestTransform, response ResponseTransform) {
	defaultRegistry.Register(from, to, request, response)
}


func Unregister(from, to Format) {
	defaultRegistry.Unregister(from, to)
}


func RegisterRequestEnvelope(from, to Format, request RequestEnvelopeTransform) {
	defaultRegistry.RegisterRequestEnvelope(from, to, request)
}


func SetPluginHooks(hooks PluginHooks) {
	defaultRegistry.SetPluginHooks(hooks)
}


func HasPluginHooks() bool {
	return defaultRegistry.HasPluginHooks()
}


func TranslateRequest(from, to Format, model string, rawJSON []byte, stream bool) []byte {
	return defaultRegistry.TranslateRequest(from, to, model, rawJSON, stream)
}


func TranslateRequestEnvelope(ctx context.Context, from, to Format, req RequestEnvelope) RequestEnvelope {
	return defaultRegistry.TranslateRequestEnvelope(ctx, from, to, req)
}


func NormalizeRequest(ctx context.Context, from, to Format, model string, body []byte, stream bool) []byte {
	return defaultRegistry.NormalizeRequest(ctx, from, to, model, body, stream)
}


func HasRequestTransformer(from, to Format) bool {
	return defaultRegistry.HasRequestTransformer(from, to)
}


func HasResponseTransformer(from, to Format) bool {
	return defaultRegistry.HasResponseTransformer(from, to)
}


func HasStreamResponseTransformer(from, to Format) bool {
	return defaultRegistry.HasStreamResponseTransformer(from, to)
}


func HasNonStreamResponseTransformer(from, to Format) bool {
	return defaultRegistry.HasNonStreamResponseTransformer(from, to)
}


func HasTokenCountTransformer(from, to Format) bool {
	return defaultRegistry.HasTokenCountTransformer(from, to)
}


func TranslateStream(ctx context.Context, from, to Format, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	return defaultRegistry.TranslateStream(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, rawJSON, param)
}


func TranslateNonStream(ctx context.Context, from, to Format, model string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	return defaultRegistry.TranslateNonStream(ctx, from, to, model, originalRequestRawJSON, requestRawJSON, rawJSON, param)
}


func TranslateTokenCount(ctx context.Context, from, to Format, count int64, rawJSON []byte) []byte {
	return defaultRegistry.TranslateTokenCount(ctx, from, to, count, rawJSON)
}

func hasAnyResponseTransform(fn ResponseTransform) bool {
	return fn.Stream != nil || fn.NonStream != nil || fn.TokenCount != nil
}
