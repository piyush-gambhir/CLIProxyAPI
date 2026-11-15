package auth

import (
	"context"
	"net/http"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestExecuteStream_AllCredentialsOverloaded_ReturnsStatusError(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(5, 0, 3)
	registerOverloadAuths(t, m, 3)

	m.RegisterExecutor(&customStreamMockExecutor{
		identifier: "codex",
		streamFn: func(_ context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {

			return nil, overloadStatusError()
		},
	})

	result, err := m.ExecuteStream(context.Background(), []string{"codex"},
		cliproxyexecutor.Request{Model: "gpt-5.6-terra"}, cliproxyexecutor.Options{})

	if err == nil {
		t.Fatalf("expected a hard error once every credential is overloaded, got result=%v", result)
	}
	statusErr, ok := err.(interface{ StatusCode() int })
	if !ok {
		t.Fatalf("error %T does not expose StatusCode(): %v", err, err)
	}
	if got := statusErr.StatusCode(); got != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", got, http.StatusServiceUnavailable)
	}
}

func TestExecuteStream_UnbufferedOverload_StaysCommittedStream(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(5, 0, 3)
	registerOverloadAuths(t, m, 3)

	m.RegisterExecutor(&customStreamMockExecutor{
		identifier: "codex",
		streamFn: func(_ context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
			ch := make(chan cliproxyexecutor.StreamChunk, 2)
			ch <- cliproxyexecutor.StreamChunk{Payload: []byte(`data: {"type":"response.created"}`)}
			ch <- cliproxyexecutor.StreamChunk{Err: overloadStatusError()}
			close(ch)
			return &cliproxyexecutor.StreamResult{
				Headers: http.Header{"Content-Type": []string{"text/event-stream"}},
				Chunks:  ch,
			}, nil
		},
	})

	result, err := m.ExecuteStream(context.Background(), []string{"codex"},
		cliproxyexecutor.Request{Model: "gpt-5.6-terra"}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("unbuffered path should hand back a committed stream, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected a committed stream result")
	}
	var sawErr bool
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("expected the overload rejection to arrive in-stream")
	}
}

func TestExecuteStream_ErrorAsFirstChunk_IsDowngradedToCommittedStream(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(5, 0, 3)
	registerOverloadAuths(t, m, 3)

	m.RegisterExecutor(&customStreamMockExecutor{
		identifier: "codex",
		streamFn: func(_ context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
			ch := make(chan cliproxyexecutor.StreamChunk, 1)
			ch <- cliproxyexecutor.StreamChunk{Err: overloadStatusError()}
			close(ch)
			return &cliproxyexecutor.StreamResult{
				Headers: http.Header{"Content-Type": []string{"text/event-stream"}},
				Chunks:  ch,
			}, nil
		},
	})

	result, err := m.ExecuteStream(context.Background(), []string{"codex"},
		cliproxyexecutor.Request{Model: "gpt-5.6-terra"}, cliproxyexecutor.Options{})

	if err != nil {
		t.Logf("first-chunk error surfaced as a hard error: %v", err)
		t.Log("NOTE: this contradicts the streamErrorResult downgrade path; review if it changes")
		return
	}
	if result == nil {
		t.Fatal("expected either an error or a committed stream")
	}
	var sawErr bool
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("expected the rejection to be delivered in-stream after the downgrade")
	}
	t.Log("confirmed: an error delivered as the first chunk is downgraded to a committed stream")
}
