package helps

import (
	"bytes"
	"context"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

const ApplyPatchUpstreamErrorMessage = "Invalid apply_patch tool arguments received from upstream."

func TranslatedClaudeErrorPayload(responseFormat sdktranslator.Format, out []byte) (string, bool) {
	if responseFormat != sdktranslator.FormatClaude || len(out) == 0 {
		return "", false
	}
	if ojson.GetBytesNoCopy(out, "type").String() != "error" {
		return "", false
	}
	return string(out), true
}

func TranslatedClaudeStreamErrorPayload(responseFormat sdktranslator.Format, lines [][]byte) (string, bool) {
	if responseFormat != sdktranslator.FormatClaude {
		return "", false
	}
	for _, chunk := range lines {
		for _, line := range bytes.Split(chunk, []byte("\n")) {
			payload := JSONPayload(line)
			if len(payload) == 0 {
				continue
			}
			if ojson.GetBytesNoCopy(payload, "type").String() == "error" {
				return string(payload), true
			}
		}
	}
	return "", false
}

func CaptureTranslatedClaudeStreamError(responseFormat sdktranslator.Format, lines [][]byte, retainedErr *error, buildErr func(string) error) {
	if retainedErr == nil || *retainedErr != nil {
		return
	}
	errPayload, okErr := TranslatedClaudeStreamErrorPayload(responseFormat, lines)
	if !okErr {
		return
	}
	if buildErr != nil {
		*retainedErr = buildErr(errPayload)
	}
}

func PublishTranslatedClaudeStreamFailure(ctx context.Context, retainedErr error, reporter *UsageReporter, streamUsage *StreamUsageBuffer) bool {
	if retainedErr == nil {
		return false
	}
	if streamUsage != nil && streamUsage.PublishFailure(ctx, reporter, retainedErr) {
		return true
	}
	if reporter != nil {
		reporter.PublishFailure(ctx, retainedErr)
	}
	return true
}

func StopTranslatedClaudeStream(ctx context.Context, retainedErr *error, reporter *UsageReporter, streamUsage *StreamUsageBuffer, out chan<- cliproxyexecutor.StreamChunk) bool {
	if retainedErr == nil || *retainedErr == nil {
		return false
	}
	err := *retainedErr
	*retainedErr = nil
	PublishTranslatedClaudeStreamFailure(ctx, err, reporter, streamUsage)
	select {
	case out <- cliproxyexecutor.StreamChunk{Err: err}:
		return true
	default:
	}
	select {
	case out <- cliproxyexecutor.StreamChunk{Err: err}:
	case <-ctx.Done():
	}
	return true
}

func ApplyPatchTranslationError(param any) error {
	state, okState := param.(interface{ ToolInputError() error })
	if !okState {
		return nil
	}
	return state.ToolInputError()
}

func FinalizeApplyPatchStream(param any) [][]byte {
	state, okState := param.(interface{ FinalizeToolInput() [][]byte })
	if !okState {
		return nil
	}
	return state.FinalizeToolInput()
}

func CanFinalizeResponseStream(param any) bool {
	state, okState := param.(interface{ CanFinalizeResponseStream() bool })
	return okState && state.CanFinalizeResponseStream()
}

func RecordApplyPatchStreamFailure(ctx context.Context, param any, reporter *UsageReporter, gatewayErr error) bool {
	return RecordApplyPatchStreamFailureWithUsage(ctx, param, reporter, nil, gatewayErr)
}

func RecordApplyPatchStreamFailureWithUsage(ctx context.Context, param any, reporter *UsageReporter, streamUsage *StreamUsageBuffer, gatewayErr error) bool {
	if ApplyPatchTranslationError(param) == nil {
		return false
	}
	if streamUsage != nil && streamUsage.PublishFailure(ctx, reporter, gatewayErr) {
		return true
	}
	reporter.PublishFailure(ctx, gatewayErr)
	return true
}

func StopApplyPatchStream(ctx context.Context, param any, reporter *UsageReporter, out chan<- cliproxyexecutor.StreamChunk, gatewayErr error) bool {
	return StopApplyPatchStreamWithUsage(ctx, param, reporter, nil, out, gatewayErr)
}

func StopApplyPatchStreamWithUsage(ctx context.Context, param any, reporter *UsageReporter, streamUsage *StreamUsageBuffer, out chan<- cliproxyexecutor.StreamChunk, gatewayErr error) bool {
	if !RecordApplyPatchStreamFailureWithUsage(ctx, param, reporter, streamUsage, gatewayErr) {
		return false
	}
	select {
	case out <- cliproxyexecutor.StreamChunk{Err: gatewayErr}:
	case <-ctx.Done():
	}
	return true
}

func EndApplyPatchStream(ctx context.Context, param any, reporter *UsageReporter, out chan<- cliproxyexecutor.StreamChunk, gatewayErr error) bool {
	chunks := FinalizeApplyPatchStream(param)
	RecordApplyPatchStreamFailure(ctx, param, reporter, gatewayErr)
	for _, chunk := range chunks {
		select {
		case out <- cliproxyexecutor.StreamChunk{Payload: chunk}:
		case <-ctx.Done():
			return true
		}
	}
	return StopApplyPatchStream(ctx, param, reporter, out, gatewayErr)
}

func ApplyPatchRequested(original []byte) bool {
	for _, identity := range util.ResponsesToolReverseIdentityMap(original) {
		if identity.ApplyPatch {
			return true
		}
	}
	return false
}

func IsApplyPatchUpstreamTool(original []byte, name string) bool {
	return util.ResponsesToolReverseIdentityMap(original)[name].ApplyPatch
}

func InitializeApplyPatchStream(ctx context.Context, from, to sdktranslator.Format, model string, original, effective []byte, param *any) {
	if to != sdktranslator.FormatOpenAIResponse || !ApplyPatchRequested(original) {
		return
	}
	_ = sdktranslator.TranslateStream(ctx, from, to, model, original, effective, nil, param)
}

func ApplyPatchOriginalRequest(req cliproxyexecutor.Request, opts cliproxyexecutor.Options) []byte {
	if len(opts.OriginalRequest) > 0 {
		return opts.OriginalRequest
	}
	return req.Payload
}
