package executor

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

const (
	codexOverloadEvent      = `{"type":"error","error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later.","param":null},"sequence_number":2}`
	codexCapacityEvent      = `{"type":"error","error":{"message":"Selected model is at capacity. Please try a different model."},"sequence_number":2}`
	codexInvalidEvent       = `{"type":"error","error":{"type":"invalid_request_error","code":"invalid_value","message":"Invalid input."},"sequence_number":2}`
	codexCreatedEvent       = `{"type":"response.created","response":{"id":"resp_1","model":"gpt-5.6-terra"}}`
	codexInProgressEvent    = `{"type":"response.in_progress","response":{"id":"resp_1"}}`
	codexOutputAddedEvent   = `{"type":"response.output_item.added","item":{"id":"msg_1","type":"message","role":"assistant","content":[]},"output_index":0}`
	codexKeepaliveEvent     = `{"type":"keepalive","sequence_number":1}`
	codexOutputDeltaEvent   = `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"hi"}`
	codexCompletedEventBody = `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`
)

func codexBufferingConfig(enabled bool) *config.Config {
	return &config.Config{Codex: config.CodexConfig{StreamBootstrapBuffering: enabled}}
}

func codexBufferingConfigWithTimeout(enabled bool, timeout string) *config.Config {
	return &config.Config{Codex: config.CodexConfig{StreamBootstrapBuffering: enabled, StreamBootstrapTimeout: timeout}}
}

func codexTestAuth(baseURL string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{Attributes: map[string]string{"base_url": baseURL, "api_key": "test"}}
}

func codexTestRequest() (cliproxyexecutor.Request, cliproxyexecutor.Options) {
	return cliproxyexecutor.Request{
			Model:   "gpt-5.6-terra",
			Payload: []byte(`{"model":"gpt-5.6-terra","input":"hello"}`),
		}, cliproxyexecutor.Options{
			SourceFormat: sdktranslator.FromString("openai-response"),
			Stream:       true,
		}
}

func codexSSEServer(events ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			eventType := "message"
			if parsed := strings.SplitN(event, `"type":"`, 2); len(parsed) == 2 {
				eventType = strings.SplitN(parsed[1], `"`, 2)[0]
			}
			_, _ = w.Write([]byte("event: " + eventType + "\n"))
			_, _ = w.Write([]byte("data: " + event + "\n\n"))
		}
	}))
}

func codexWebsocketServer(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, errRead := conn.ReadMessage(); errRead != nil {
			t.Errorf("read websocket message: %v", errRead)
			return
		}
		for _, frame := range frames {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(frame))
		}
	}))
}

func codexWebsocketRequest() (cliproxyexecutor.Request, cliproxyexecutor.Options) {
	return cliproxyexecutor.Request{
			Model:   "gpt-5.6-terra",
			Payload: []byte(`{"model":"gpt-5.6-terra","input":[{"type":"message","role":"user","content":"hello"}]}`),
		}, cliproxyexecutor.Options{
			SourceFormat: sdktranslator.FromString("openai-response"),
		}
}

func drainChunks(result *cliproxyexecutor.StreamResult) (string, error) {
	var payloads [][]byte
	var streamErr error
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			if streamErr == nil {
				streamErr = chunk.Err
			}
			continue
		}
		payloads = append(payloads, chunk.Payload)
	}
	return string(bytes.Join(payloads, []byte("\n"))), streamErr
}

func TestCodexExecutor_BootstrapBuffering_OverloadFailsAttemptWithoutLeakingHandshake(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, codexOverloadEvent)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err == nil {
		t.Fatal("expected ExecuteStream to fail the attempt on an overload rejection")
	}
	if result != nil {
		t.Fatal("expected nil result so no buffered handshake chunk can reach the client")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d (upstream hides 503 behind HTTP 200)", got, http.StatusServiceUnavailable)
	}
}

func TestCodexExecutor_BootstrapBuffering_CapacityFailsAttemptWithoutLeakingHandshake(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, codexCapacityEvent)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err == nil {
		t.Fatal("expected ExecuteStream to fail the attempt on a capacity rejection")
	}
	if result != nil {
		t.Fatal("expected nil result so no buffered handshake chunk can reach the client")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusTooManyRequests {
		t.Fatalf("status code = %d, want %d", got, http.StatusTooManyRequests)
	}
}

func TestCodexExecutor_BootstrapBuffering_NonOverloadStaysInStream(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, codexInvalidEvent)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err != nil {
		t.Fatalf("non-overload failure must not fail the attempt synchronously: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result for in-stream error delivery")
	}
	combined, streamErr := drainChunks(result)
	if streamErr == nil {
		t.Fatal("expected the invalid-request failure to arrive as an in-stream chunk error")
	}
	if !strings.Contains(combined, `"type":"response.created"`) {
		t.Fatalf("buffered handshake must be flushed before the in-stream error: %s", combined)
	}
	if got := statusCodeFromTestError(t, streamErr); got != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestCodexExecutor_BootstrapBuffering_BufferLimitReleasesStream(t *testing.T) {
	events := make([]string, 0, codexBootstrapMaxBufferedFrames+2)
	for i := 0; i < codexBootstrapMaxBufferedFrames+1; i++ {
		events = append(events, fmt.Sprintf(`{"type":"response.in_progress","response":{"id":"resp_%d"}}`, i))
	}
	events = append(events, codexOverloadEvent)
	server := codexSSEServer(events...)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err != nil {
		t.Fatalf("expected the stream to be released once the buffer limit is hit: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result after the buffer limit released the stream")
	}
	_, streamErr := drainChunks(result)
	if streamErr == nil {
		t.Fatal("expected the overload error to be delivered in-stream after the limit was hit")
	}
}

func codexWebsocketRawServer(t *testing.T, frames []string, tail ...string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, errRead := conn.ReadMessage(); errRead != nil {
			return
		}
		for _, frame := range frames {
			if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(frame)); errWrite != nil {
				return
			}
		}
		for _, frame := range tail {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(frame))
		}
	}))
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_FrameBudgetReleasesStream(t *testing.T) {
	frames := make([]string, 0, codexBootstrapMaxBufferedFrames+1)
	for i := 0; i < codexBootstrapMaxBufferedFrames+1; i++ {
		frames = append(frames, codexInProgressEvent)
	}
	server := codexWebsocketRawServer(t, frames, codexOverloadEvent)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("the frame budget must release the stream before the overload arrives: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result once the frame budget released the stream")
	}
	drainChunks(result)
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_BudgetBoundaryIsExact(t *testing.T) {
	send := func(n int) error {
		frames := make([]string, n)
		for i := range frames {
			frames[i] = codexInProgressEvent
		}
		server := codexWebsocketRawServer(t, frames, codexOverloadEvent)
		defer server.Close()
		req, opts := codexWebsocketRequest()
		result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
		if result != nil {
			drainChunks(result)
		}
		return err
	}
	if err := send(codexBootstrapMaxBufferedFrames - 1); err == nil {
		t.Fatalf("%d messages are inside the budget and must still fail over", codexBootstrapMaxBufferedFrames-1)
	}

	if err := send(codexBootstrapMaxBufferedFrames); err == nil {
		t.Fatalf("%d messages exactly fill the budget and must still fail over", codexBootstrapMaxBufferedFrames)
	}
	if err := send(codexBootstrapMaxBufferedFrames + 1); err != nil {
		t.Fatalf("%d messages exceed the budget and must release: %v", codexBootstrapMaxBufferedFrames+1, err)
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_ByteCapOrderingAndSeed(t *testing.T) {
	t.Run("oversized message is not admitted", func(t *testing.T) {
		oversized := `{"type":"response.in_progress","response":{"id":"` + strings.Repeat("q", codexBootstrapMaxBufferedBytes*2) + `"}}`
		server := codexWebsocketRawServer(t, []string{oversized}, codexOverloadEvent)
		defer server.Close()

		req, opts := codexWebsocketRequest()
		result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
		if err != nil {
			t.Fatalf("a message larger than the cap must be released, not admitted: %v", err)
		}
		if result == nil {
			t.Fatal("expected a stream result")
		}
		drainChunks(result)
	})

	t.Run("budget counts the upstream message", func(t *testing.T) {
		frame := `{"type":"response.in_progress","response":{"id":"` + strings.Repeat("w", codexBootstrapMaxBufferedBytes/8) + `"}}`
		frames := make([]string, 10)
		for i := range frames {
			frames[i] = frame
		}
		server := codexWebsocketRawServer(t, frames, codexOverloadEvent)
		defer server.Close()

		req, _ := codexWebsocketRequest()
		opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")}
		result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
		if err != nil {
			t.Fatalf("ten messages of cap/8 must exhaust the byte budget: %v", err)
		}
		if result == nil {
			t.Fatal("expected a stream result")
		}
		drainChunks(result)
	})
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_BudgetFrameIsNotDropped(t *testing.T) {
	frames := make([]string, 0, codexBootstrapMaxBufferedFrames+2)
	for i := 0; i < codexBootstrapMaxBufferedFrames; i++ {
		frames = append(frames, codexInProgressEvent)
	}
	frames = append(frames, `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"FIRSTTOKEN"}`)
	server := codexWebsocketRawServer(t, frames, codexCompletedEventBody)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("unexpected ExecuteStream error: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result")
	}
	combined, _ := drainChunks(result)
	if !strings.Contains(combined, "FIRSTTOKEN") {
		t.Fatalf("the frame that tripped the budget was dropped: %s", combined)
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_SkippedFramesExhaustTheWindow(t *testing.T) {
	cases := []struct {
		name  string
		frame string
	}{
		{"whitespace only frames", "   \n\t "},
		{"empty frames", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := make([]string, codexBootstrapMaxBufferedFrames+2)
			for i := range frames {
				frames[i] = tc.frame
			}
			server := codexWebsocketRawServer(t, frames, codexOverloadEvent)
			defer server.Close()

			req, opts := codexWebsocketRequest()
			result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

			if err != nil {
				t.Fatalf("the window must release before the overload arrives, got failover: %v", err)
			}
			if result == nil {
				t.Fatal("expected a stream result once the window was exhausted")
			}
			drainChunks(result)
		})
	}
}

func TestCodexBootstrapBudgetsStaySmall(t *testing.T) {

	if codexBootstrapMaxBufferedFrames != 48 {
		t.Fatalf("frame budget = %d, want 48; changing it changes how long the headers are held, so update config.example.yaml too", codexBootstrapMaxBufferedFrames)
	}
	if codexBootstrapMaxBufferedBytes != 1<<20 {
		t.Fatalf("byte budget = %d, want 1MiB", codexBootstrapMaxBufferedBytes)
	}
}

func TestCodexExecutor_BootstrapBuffering_InStreamDeliveryNeedsRenderedFrames(t *testing.T) {
	incomplete := `{"type":"response.incomplete","response":{"id":"resp_1","output":[],"usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1}}}`
	server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, codexOutputAddedEvent, incomplete)
	defer server.Close()

	req, _ := codexTestRequest()
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: true}
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("the executor still returns a stream; it is the conductor that cannot commit it: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result")
	}
	combined, streamErr := drainChunks(result)
	if streamErr == nil {
		t.Fatal("expected the failure to arrive as an in-stream chunk error")
	}
	if combined != "" {
		t.Fatalf("Chat Completions renders these frames as nothing, so no payload precedes the error: %q", combined)
	}
}

func TestCodexExecutor_BootstrapBuffering_NonOverloadTerminalStaysInStream(t *testing.T) {
	cases := []struct{ name, tail string }{
		{"empty incomplete", `{"type":"response.incomplete","response":{"id":"resp_1","output":[],"usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, codexOutputAddedEvent, tc.tail)
			defer server.Close()

			req, opts := codexTestRequest()
			result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

			if err != nil {
				t.Fatalf("a non-overload terminal failure must not fail the attempt over: %v", err)
			}
			if result == nil {
				t.Fatal("expected a stream result carrying the buffered frames and the error")
			}
			combined, streamErr := drainChunks(result)
			if streamErr == nil {
				t.Fatal("expected the failure to arrive as an in-stream chunk error")
			}
			if !strings.Contains(combined, `"type":"response.created"`) {
				t.Fatalf("buffered frames must be flushed before the in-stream error: %s", combined)
			}
		})
	}
}

func TestCodexExecutor_BootstrapBuffering_OversizedFrameIsNotAdmitted(t *testing.T) {
	oversized := `{"type":"response.in_progress","response":{"id":"` + strings.Repeat("q", codexBootstrapMaxBufferedBytes*2) + `"}}`
	body := "data: " + oversized + "\n" + "data: " + codexOverloadEvent + "\n\n"
	server := codexSSERawServer(body)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("a frame larger than the cap must be released, not admitted: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result once the oversized frame released the stream")
	}
	drainChunks(result)
}

func TestCodexExecutor_BootstrapBuffering_ByteCapCountsUpstreamFrames(t *testing.T) {
	frame := `{"type":"response.in_progress","response":{"id":"` + strings.Repeat("w", codexBootstrapMaxBufferedBytes/8) + `"}}`
	body := strings.Repeat("data: "+frame+"\n\n", 10) + "data: " + codexOverloadEvent + "\n\n"
	server := codexSSERawServer(body)
	defer server.Close()

	req, _ := codexTestRequest()
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: true}
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("ten frames of cap/8 must exhaust the byte budget: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result once the byte budget released the stream")
	}
	drainChunks(result)
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_ByteCapReleasesStream(t *testing.T) {
	third := `{"type":"response.in_progress","response":{"id":"` + strings.Repeat("z", codexBootstrapMaxBufferedBytes/3) + `"}}`
	server := codexWebsocketRawServer(t, []string{third, third, third, third}, codexOverloadEvent)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err != nil {
		t.Fatalf("the websocket byte cap must release the stream before the overload arrives: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result once the byte cap released the stream")
	}
	drainChunks(result)
}

func codexSSERawServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
}

func TestCodexExecutor_BootstrapBuffering_BudgetBoundaryIsExact(t *testing.T) {
	overload := "data: " + codexOverloadEvent + "\n\n"

	t.Run("one line under the budget still fails over", func(t *testing.T) {
		body := strings.Repeat(": keepalive\n", codexBootstrapMaxBufferedFrames-1) + overload
		server := codexSSERawServer(body)
		defer server.Close()

		req, opts := codexTestRequest()
		_, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
		if err == nil {
			t.Fatalf("%d held lines are inside the %d-line budget and must still fail over", codexBootstrapMaxBufferedFrames-1, codexBootstrapMaxBufferedFrames)
		}
	})

	t.Run("exactly the budget still fails over", func(t *testing.T) {
		body := strings.Repeat(": keepalive\n", codexBootstrapMaxBufferedFrames) + overload
		server := codexSSERawServer(body)
		defer server.Close()

		req, opts := codexTestRequest()
		_, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
		if err == nil {
			t.Fatalf("%d held lines fill the budget exactly and must still fail over", codexBootstrapMaxBufferedFrames)
		}
	})

	t.Run("one line over the budget releases", func(t *testing.T) {
		body := strings.Repeat(": keepalive\n", codexBootstrapMaxBufferedFrames+1) + overload
		server := codexSSERawServer(body)
		defer server.Close()

		req, opts := codexTestRequest()
		result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
		if err != nil {
			t.Fatalf("%d held lines exceed the %d-line budget and must release: %v", codexBootstrapMaxBufferedFrames+1, codexBootstrapMaxBufferedFrames, err)
		}
		if result == nil {
			t.Fatal("expected a stream result once the budget released the stream")
		}
		drainChunks(result)
	})
}

func TestCodexExecutor_BootstrapBuffering_EmptyDataFrameDoesNotReleaseStream(t *testing.T) {
	for _, frame := range []string{"data:\n\n", "data: \n\n", "data:   \t\n\n"} {
		body := frame + "data: " + codexOverloadEvent + "\n\n"
		server := codexSSERawServer(body)

		req, opts := codexTestRequest()
		result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
		if err == nil {
			t.Errorf("an empty data frame %q must keep the bootstrap window open", frame)
		}
		if result != nil {
			t.Errorf("frame %q: expected nil result so no buffered chunk can reach the client", frame)
		}
		server.Close()
	}
}

func TestCodexExecutor_BootstrapBuffering_BoundHoldsForEverySSEFraming(t *testing.T) {
	overload := "data: " + codexOverloadEvent + "\n\n"
	handshake := `{"type":"response.in_progress","response":{"id":"resp_1"}}`

	cases := []struct {
		name string
		body string
	}{
		{

			name: "comment heartbeats without blank separators",
			body: strings.Repeat(": keepalive\n", codexBootstrapMaxBufferedFrames*4) + overload,
		},
		{

			name: "data frames without blank separators",
			body: strings.Repeat("data: "+handshake+"\n", codexBootstrapMaxBufferedFrames*4) + overload,
		},
		{

			name: "three line framing",
			body: strings.Repeat("event: response.in_progress\ndata: "+handshake+"\n\n", codexBootstrapMaxBufferedFrames*4) + overload,
		},
		{

			name: "double blank separators",
			body: strings.Repeat("data: "+handshake+"\n\n\n", codexBootstrapMaxBufferedFrames*4) + overload,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := codexSSERawServer(tc.body)
			defer server.Close()

			req, _ := codexTestRequest()

			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: true}
			result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

			if err != nil {
				t.Fatalf("the bound must release the stream before the overload arrives, got failover: %v", err)
			}
			if result == nil {
				t.Fatal("expected a stream result once the bound released the stream")
			}
			if _, streamErr := drainChunks(result); streamErr == nil {
				t.Fatal("expected the overload to arrive in-stream after the bound released the stream")
			}
		})
	}
}

func TestCodexExecutor_BootstrapBuffering_ByteCapReleasesStream(t *testing.T) {

	third := `{"type":"response.in_progress","response":{"id":"` + strings.Repeat("y", codexBootstrapMaxBufferedBytes/3) + `"}}`
	body := "data: " + third + "\n\n" + "data: " + third + "\n\n" + "data: " + third + "\n\n" +
		"data: " + third + "\n\n" + "data: " + codexOverloadEvent + "\n\n"
	server := codexSSERawServer(body)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err != nil {
		t.Fatalf("the byte cap must release the stream before the overload arrives, got failover: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result once the byte cap released the stream")
	}
	drainChunks(result)
}

func TestCodexExecutor_BootstrapBuffering_NonContentFramesDoNotReleaseStream(t *testing.T) {
	cases := []struct{ name, frame string }{
		{"keepalive", codexKeepaliveEvent},
		{"output item added", codexOutputAddedEvent},
		{"content part added", `{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`},
		{"reasoning summary part added", `{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, tc.frame, codexOverloadEvent)
			defer server.Close()

			req, opts := codexTestRequest()
			result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

			if err == nil {
				t.Fatalf("a %s frame must keep the bootstrap window open", tc.name)
			}
			if result != nil {
				t.Fatal("expected nil result so no buffered chunk can reach the client")
			}
			if got := statusCodeFromTestError(t, err); got != http.StatusServiceUnavailable {
				t.Fatalf("status code = %d, want %d", got, http.StatusServiceUnavailable)
			}
		})
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_NonContentFramesDoNotReleaseStream(t *testing.T) {
	for _, frame := range []string{codexKeepaliveEvent, codexOutputAddedEvent} {
		server := codexWebsocketServer(t, codexCreatedEvent, codexInProgressEvent, frame, codexOverloadEvent)
		req, opts := codexWebsocketRequest()
		result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
		if err == nil || result != nil {
			t.Fatalf("frame %s must keep the websocket bootstrap window open", frame)
		}
		server.Close()
	}
}

func TestCodexExecutor_BootstrapBuffering_ContentFrameReleasesBeforeOverload(t *testing.T) {
	for _, tc := range codexReleasingFrameCases() {
		t.Run(tc.name, func(t *testing.T) {
			server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, tc.frame, codexOverloadEvent)
			defer server.Close()

			req, opts := codexTestRequest()
			result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
			if err != nil {
				t.Fatalf("a %s frame must release the stream, not fail the attempt over: %v", tc.name, err)
			}
			if result == nil {
				t.Fatalf("a %s frame must release the stream", tc.name)
			}
			if _, streamErr := drainChunks(result); streamErr == nil {
				t.Fatalf("the overload after a %s frame must be delivered in-stream", tc.name)
			}
		})
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_ContentFrameReleasesBeforeOverload(t *testing.T) {
	for _, tc := range codexReleasingFrameCases() {
		t.Run(tc.name, func(t *testing.T) {
			server := codexWebsocketServer(t, codexCreatedEvent, codexInProgressEvent, tc.frame, codexOverloadEvent)
			defer server.Close()

			req, opts := codexWebsocketRequest()
			result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
			if err != nil {
				t.Fatalf("a %s frame must release the stream, not fail the attempt over: %v", tc.name, err)
			}
			if result == nil {
				t.Fatalf("a %s frame must release the stream", tc.name)
			}
			if _, streamErr := drainChunks(result); streamErr == nil {
				t.Fatalf("the overload after a %s frame must be delivered in-stream", tc.name)
			}
		})
	}
}

func codexReleasingFrameCases() []struct{ name, frame string } {
	return []struct{ name, frame string }{
		{"output text delta", codexOutputDeltaEvent},
		{"web search call announced", `{"type":"response.output_item.added","item":{"id":"ws_1","type":"web_search_call","status":"in_progress"},"output_index":0}`},
	}
}

func TestCodexExecutor_BootstrapBuffering_CancelDuringBootstrapIsNotAnUpstreamFailure(t *testing.T) {
	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: " + codexCreatedEvent + "\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-released
	}))
	defer server.Close()
	defer close(released)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(ctx, codexTestAuth(server.URL), req, opts)
	if result != nil {
		drainChunks(result)
	}

	if err != context.Canceled {
		t.Fatalf("a cancelled caller must surface as context.Canceled itself, got %T: %v", err, err)
	}
}

func TestCodexExecutor_BootstrapBuffering_HeartbeatArithmeticPerFraming(t *testing.T) {
	overload := "event: error\ndata: " + codexOverloadEvent + "\n\n"
	cases := []struct {
		name      string
		heartbeat string
		protected int
	}{
		{"three-line event:/data:/blank", "event: keepalive\ndata: " + codexKeepaliveEvent + "\n\n", 15},
		{"two-line : keepalive comment", ": keepalive\n\n", 23},
		{"one-line : keepalive comment", ": keepalive\n", 47},
	}
	failsOver := func(t *testing.T, heartbeats int, heartbeat, overload string) bool {
		t.Helper()
		server := codexSSERawServer(strings.Repeat(heartbeat, heartbeats) + overload)
		defer server.Close()
		req, opts := codexTestRequest()
		result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
		if result != nil {
			drainChunks(result)
		}
		return err != nil
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !failsOver(t, tc.protected, tc.heartbeat, overload) {
				t.Fatalf("%d heartbeats of the %s framing must still fail over; config.example.yaml documents %d", tc.protected, tc.name, tc.protected)
			}
			if failsOver(t, tc.protected+1, tc.heartbeat, overload) {
				t.Fatalf("%d heartbeats of the %s framing exceed the budget and must release the stream", tc.protected+1, tc.name)
			}
		})
	}
}

func TestCodexExecutor_BootstrapBuffering_ByteCapCountsTranslatedChunks(t *testing.T) {
	padded := `{"type":"response.in_progress","response":{"id":"` + strings.Repeat("p", 300<<10) + `"}}`
	server := codexSSEServer(padded, padded, codexOverloadEvent)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("two frames whose upstream bytes fit the cap but whose chunks do not must release the stream: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result once the byte budget released the stream")
	}
	if _, streamErr := drainChunks(result); streamErr == nil {
		t.Fatal("expected the overload to arrive in-stream after the byte budget released")
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_ByteCapCountsTranslatedChunks(t *testing.T) {
	padded := `{"type":"response.in_progress","response":{"id":"` + strings.Repeat("p", 300<<10) + `"}}`
	server := codexWebsocketRawServer(t, []string{padded, padded}, codexOverloadEvent)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("two frames whose upstream bytes fit the cap but whose chunks do not must release the stream: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result once the byte budget released the stream")
	}
	if _, streamErr := drainChunks(result); streamErr == nil {
		t.Fatal("expected the overload to arrive in-stream after the byte budget released")
	}
}

func TestIsCodexBootstrapBufferableEvent(t *testing.T) {
	hold := []string{
		`{"type":"response.created"}`,
		`{"type":"response.in_progress"}`,
		`{"type":"codex.rate_limits"}`,
		`{"type":"codex.response.metadata"}`,
		`{"type":"response.metadata","response_id":"resp_1","sequence_number":2,"metadata":{"openai_chatgpt_moderation_metadata":{"prompt":{}}}}`,
		codexKeepaliveEvent,
		codexOutputAddedEvent,
		`{"type":"response.output_item.added","item":{"id":"rs_1","type":"reasoning"}}`,
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","arguments":""}}`,
		`{"type":"response.output_item.added","item":{"id":"ct_1","type":"custom_tool_call","input":""}}`,
		`{"type":"response.output_item.added","item":{"id":"msg_2","type":"message","content":[{"type":"output_text","text":""}]}}`,
		`{"type":"response.output_item.added","item":{"id":"msg_3","type":"message","content":[{"type":"refusal","refusal":""}]}}`,
		`{"type":"response.output_item.added","item":{"id":"rs_2","type":"reasoning","summary":[{"type":"summary_text","text":""}]}}`,
		`{"type":"response.output_item.added","item":{"id":"rs_3","type":"reasoning","content":[{"type":"reasoning_text","text":""}]}}`,
		`{"type":"response.content_part.added","part":{"type":"text","text":""}}`,
		`{"type":"response.reasoning_summary_part.added","part":{"type":"reasoning_text","text":""}}`,
		`{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`,
		`{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}`,
		``,
		`   `,
	}
	for _, payload := range hold {
		eventType := ojson.GetBytes([]byte(payload), "type").String()
		if !isCodexBootstrapBufferableEvent(eventType, []byte(payload)) {
			t.Errorf("must stay bufferable: %s", payload)
		}
	}

	release := []string{
		`{"type":"response.output_text.delta","delta":"hi"}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"x"}`,
		`{"type":"response.shell_call_command.delta","delta":"ls"}`,
		`{"type":"response.shell_call_output_content.delta","delta":{"stdout":"x"}}`,
		`{"type":"response.shell_call_output_content.done","output":[]}`,
		`{"type":"response.output_item.done","item":{"id":"m1","type":"message"}}`,
		`{"type":"response.output_item.added","item":{"id":"ws_1","type":"web_search_call","status":"in_progress"}}`,
		`{"type":"response.output_item.added","item":{"id":"fs_1","type":"file_search_call"}}`,
		`{"type":"response.output_item.added","item":{"id":"ig_1","type":"image_generation_call"}}`,
		`{"type":"response.output_item.added","item":{"id":"x_1","type":"some_future_server_tool"}}`,
		`{"type":"response.content_part.added","part":{"type":"output_text","text":"already here"}}`,
		`{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":"already here"}}`,
		`{"type":"response.content_part.added","part":{"type":"output_audio","audio":"AAAA"}}`,
		`{"type":"response.content_part.added","part":{"type":"refusal","refusal":"I cannot help"}}`,
		`{"type":"response.output_item.added","item":{"id":"rf1","type":"message","content":[{"type":"refusal","refusal":"I cannot help"}]}}`,
		`{"type":"response.output_item.added","item":{"id":"m1","type":"message","content":[{"type":"output_text","text":"already generated"}]}}`,
		`{"type":"response.output_item.added","item":{"id":"r1","type":"reasoning","summary":[{"type":"summary_text","text":"already reasoned"}]}}`,
		`{"type":"response.output_item.added","item":{"id":"f1","type":"function_call","arguments":"{\"path\":\"/\"}"}}`,
		`{"type":"response.output_item.added","item":{"id":"c1","type":"custom_tool_call","input":"already here"}}`,
		`{"type":"response.output_item.added","item":{"id":"a1","type":"message","content":[{"type":"output_audio","audio":"AAAA"}]}}`,
		`{"type":"response.output_item.added","item":{"id":"i1","type":"message","content":[{"type":"output_image","image_url":"data:x"}]}}`,
		`{"type":"response.output_item.added","item":{"id":"r2","type":"reasoning","encrypted_content":"BLOB"}}`,
		`{"type":"response.output_item.added","item":{"id":"r3","type":"reasoning","content":[{"type":"reasoning_text","text":"already reasoned"}]}}`,
		`{"type":"response.web_search_call.searching","item_id":"ws_1"}`,
		codexCompletedEventBody,
		codexOverloadEvent,
		`{"type":"response.some_future_event_we_have_never_seen"}`,
	}
	for _, payload := range release {
		eventType := ojson.GetBytes([]byte(payload), "type").String()
		if isCodexBootstrapBufferableEvent(eventType, []byte(payload)) {
			t.Errorf("must release the stream: %s", payload)
		}
	}
}

func TestCodexExecutor_BootstrapBuffering_FlushesInOrderOnFirstOutput(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, codexOutputAddedEvent, codexOutputDeltaEvent, codexCompletedEventBody)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("unexpected ExecuteStream error: %v", err)
	}

	combined, streamErr := drainChunks(result)
	if streamErr != nil {
		t.Fatalf("unexpected chunk error: %v", streamErr)
	}

	order := []string{`"type":"response.created"`, `"type":"response.in_progress"`, `"type":"response.output_item.added"`, `"type":"response.output_text.delta"`}
	prev := -1
	for _, marker := range order {
		at := strings.Index(combined, marker)
		if at < 0 {
			t.Fatalf("missing %s: %s", marker, combined)
		}
		if at <= prev {
			t.Fatalf("frames must be replayed in upstream order, %s came too early: %s", marker, combined)
		}
		prev = at
	}
	for _, marker := range order {
		if n := strings.Count(combined, marker); n != 1 {
			t.Fatalf("held frame %s was emitted %d times, want 1: %s", marker, n, combined)
		}
	}
}

func TestCodexExecutor_BootstrapBuffering_DefaultDisabledPassthrough(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent, codexOverloadEvent)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(&config.Config{}).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("default unbuffered ExecuteStream returned error at call time: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result in default unbuffered mode")
	}
	_, streamErr := drainChunks(result)
	if streamErr == nil {
		t.Fatal("expected stream error in chunks for default unbuffered mode")
	}

	if got := statusCodeFromTestError(t, streamErr); got != http.StatusBadGateway {
		t.Fatalf("status code = %d, want %d while buffering is disabled", got, http.StatusBadGateway)
	}
}

func TestCodexExecutor_BootstrapBuffering_ContextCancelDuringBootstrap(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, opts := codexTestRequest()
	_, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(ctx, codexTestAuth(server.URL), req, opts)
	if err == nil {
		t.Fatal("expected an error for a cancelled bootstrap")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected the context cancellation to surface, got: %v", err)
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_OverloadFailsAttempt(t *testing.T) {
	server := codexWebsocketServer(t, codexCreatedEvent, codexInProgressEvent, codexOverloadEvent)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err == nil {
		t.Fatal("expected ExecuteStream to fail the attempt on a websocket overload rejection")
	}
	if result != nil {
		t.Fatal("expected nil result so no buffered handshake frame can reach the client")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", got, http.StatusServiceUnavailable)
	}
}

const codexModerationMetadataEvent = `{"type":"response.metadata","response_id":"resp_1","sequence_number":2,"metadata":{"openai_chatgpt_moderation_metadata":{"prompt":{"omnimod":{"protection_type":"omnimod"}}}}}`

func TestCodexExecutor_BootstrapBuffering_OverloadAfterModerationMetadataFailsAttempt(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, codexModerationMetadataEvent, codexOverloadEvent)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err == nil {
		t.Fatal("expected ExecuteStream to fail the attempt on an overload after response.metadata")
	}
	if result != nil {
		t.Fatal("expected nil result so no buffered frame can reach the client")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", got, http.StatusServiceUnavailable)
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_OverloadAfterModerationMetadataFailsAttempt(t *testing.T) {
	server := codexWebsocketServer(t, codexCreatedEvent, codexInProgressEvent, codexModerationMetadataEvent, codexOverloadEvent)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err == nil {
		t.Fatal("expected ExecuteStream to fail the attempt on a websocket overload after response.metadata")
	}
	if result != nil {
		t.Fatal("expected nil result so no buffered frame can reach the client")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", got, http.StatusServiceUnavailable)
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_CapacityFailsAttempt(t *testing.T) {
	server := codexWebsocketServer(t, codexCreatedEvent, codexInProgressEvent, codexCapacityEvent)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err == nil {
		t.Fatal("expected ExecuteStream to fail the attempt on a websocket capacity rejection")
	}
	if result != nil {
		t.Fatal("expected nil result so no buffered frame can reach the client")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusTooManyRequests {
		t.Fatalf("status code = %d, want %d", got, http.StatusTooManyRequests)
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_PrivateHandshakeFramesDoNotExhaustWindow(t *testing.T) {
	server := codexWebsocketServer(t,
		`{"type":"codex.rate_limits","rate_limits":{"primary":{"used_percent":1}}}`,
		`{"type":"codex.response.metadata","metadata":{"conversation_id":"conv_1"}}`,
		codexCreatedEvent,
		codexInProgressEvent,
		codexOverloadEvent,
	)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err == nil {
		t.Fatal("expected the overload rejection to be caught past the private handshake frames")
	}
	if result != nil {
		t.Fatal("expected nil result so no buffered frame can reach the client")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", got, http.StatusServiceUnavailable)
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_NonOverloadStaysInStream(t *testing.T) {
	server := codexWebsocketServer(t, codexCreatedEvent, codexInProgressEvent, codexInvalidEvent)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	if err != nil {
		t.Fatalf("non-overload failure must not fail the attempt synchronously: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result for in-stream error delivery")
	}
	combined, streamErr := drainChunks(result)
	if streamErr == nil {
		t.Fatal("expected the invalid-request failure to arrive as an in-stream chunk error")
	}
	if !strings.Contains(combined, `"type":"response.created"`) {
		t.Fatalf("buffered handshake must be flushed before the in-stream error: %s", combined)
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_FlushesInOrderOnFirstOutput(t *testing.T) {
	server := codexWebsocketServer(t,
		codexCreatedEvent,
		codexInProgressEvent,
		codexOutputAddedEvent,
		codexOutputDeltaEvent,
		`{"type":"response.completed","response":{"id":"resp_1","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`,
	)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfig(true)).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("unexpected ExecuteStream error: %v", err)
	}

	combined, streamErr := drainChunks(result)
	if streamErr != nil {
		t.Fatalf("unexpected chunk error: %v", streamErr)
	}

	order := []string{`"type":"response.created"`, `"type":"response.in_progress"`, `"type":"response.output_item.added"`, `"type":"response.output_text.delta"`}
	prev := -1
	for _, marker := range order {
		at := strings.Index(combined, marker)
		if at < 0 {
			t.Fatalf("missing %s: %s", marker, combined)
		}
		if at <= prev {
			t.Fatalf("frames must be replayed in upstream order, %s came too early: %s", marker, combined)
		}
		prev = at
	}
	for _, marker := range order {
		if got := strings.Count(combined, marker); got != 1 {
			t.Fatalf("held frame %s was emitted %d times, want 1: %s", marker, got, combined)
		}
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_DefaultDisabledPassthrough(t *testing.T) {
	server := codexWebsocketServer(t, codexCreatedEvent, codexOverloadEvent)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(&config.Config{}).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("default unbuffered ExecuteStream returned error at call time: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result in default unbuffered mode")
	}
	_, streamErr := drainChunks(result)
	if streamErr == nil {
		t.Fatal("expected stream error in chunks for default unbuffered mode")
	}
	if got := statusCodeFromTestError(t, streamErr); got != http.StatusBadGateway {
		t.Fatalf("status code = %d, want %d while buffering is disabled", got, http.StatusBadGateway)
	}
}

func TestIsCodexOverloadBootstrapFailureRejectsRequestFaults(t *testing.T) {
	notOverload := []string{
		`{"error":{"type":"invalid_request_error","code":"invalid_value"}}`,
		`{"error":{"type":"authentication_error","code":"invalid_api_key"}}`,
		`{"error":{"type":"upstream_error","code":"unknown"}}`,
		`{"error":{"type":"server_error","code":"server_error","message":"An internal error occurred without retry advice"}}`,
	}
	for _, body := range notOverload {
		if isCodexOverloadBootstrapFailure([]byte(body)) {
			t.Fatalf("request-level fault must not trigger bootstrap failover: %s", body)
		}
	}
	if !isCodexOverloadBootstrapFailure([]byte(`{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded"}}`)) {
		t.Fatal("rate limit rejections should be eligible for bootstrap failover")
	}
	serverErrorFull := `{"error":{"type":"server_error","code":"server_error","message":"An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID 2f5014c9-7cfe-4fb7-813e-ecb447da3edd in your message."}}`
	if !isCodexOverloadBootstrapFailure([]byte(serverErrorFull)) {
		t.Fatal("server_error with 'You can retry your request' should be eligible for bootstrap failover")
	}
	serverErrorShort := `{"error":{"type":"server_error","code":"server_error","message":"You can retry your request"}}`
	if !isCodexOverloadBootstrapFailure([]byte(serverErrorShort)) {
		t.Fatal("short server_error with 'You can retry your request' should be eligible for bootstrap failover")
	}
	capacityError := `{"error":{"message":"Selected model is at capacity. Please try a different model."}}`
	if !isCodexOverloadBootstrapFailure([]byte(capacityError)) {
		t.Fatal("model capacity error should be eligible for bootstrap failover")
	}
	capacityErrorShort := `{"error":{"message":"Selected Model is at capacity"}}`
	if !isCodexOverloadBootstrapFailure([]byte(capacityErrorShort)) {
		t.Fatal("short model capacity error should be eligible for bootstrap failover")
	}
}

func codexWebsocketServerHoldingConnection(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, errRead := conn.ReadMessage(); errRead != nil {
			t.Errorf("read websocket message: %v", errRead)
			return
		}
		for _, frame := range frames {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(frame))
		}
		for {
			if _, _, errRead := conn.ReadMessage(); errRead != nil {
				return
			}
		}
	}))
}

func executeWebsocketStreamInSession(t *testing.T, frames ...string) (notified bool, err error) {
	t.Helper()

	server := codexWebsocketServerHoldingConnection(t, frames...)
	defer server.Close()

	exec := NewCodexWebsocketsExecutor(codexBufferingConfig(true))
	exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}

	const sessionID = "bootstrap-session"
	disconnectCh := exec.UpstreamDisconnectChan(sessionID)
	if disconnectCh == nil {
		t.Fatal("expected a disconnect channel")
	}

	req, opts := codexWebsocketRequest()
	opts.Metadata = map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: sessionID}
	_, err = exec.ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

	select {
	case <-disconnectCh:
		notified = true
	default:
	}
	return notified, err
}

func TestCodexWebsocketsExecutor_BootstrapOverload_DoesNotNotifyDownstreamDisconnect(t *testing.T) {
	notified, err := executeWebsocketStreamInSession(t, codexCreatedEvent, codexInProgressEvent, codexOverloadEvent)

	if err == nil {
		t.Fatal("expected the overload rejection to fail the attempt")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", got, http.StatusServiceUnavailable)
	}
	if notified {
		t.Fatal("bootstrap overload must not signal a downstream disconnect: the conductor still has to retry on another credential, and signalling closes the client connection with zero frames delivered")
	}
}

func TestCodexWebsocketsExecutor_BootstrapNonOverload_StillNotifiesDownstreamDisconnect(t *testing.T) {
	notified, err := executeWebsocketStreamInSession(t, codexCreatedEvent, codexInProgressEvent, codexInvalidEvent)

	if err != nil {
		t.Fatalf("non-overload failures stay in-stream, got err = %v", err)
	}
	if !notified {
		t.Fatal("a terminal failure that is delivered in-stream must still signal the downstream disconnect")
	}
}

type mockClock struct {
	mu  sync.Mutex
	cur time.Time
}

func (m *mockClock) now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cur
}

func (m *mockClock) advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cur = m.cur.Add(d)
}

func withMockClock(t *testing.T, initial time.Time) *mockClock {
	m := &mockClock{cur: initial}
	cleanup := setCodexBootstrapNowForTest(m.now)
	t.Cleanup(cleanup)
	return m
}

func TestCodexConfig_StreamBootstrapTimeoutDuration(t *testing.T) {
	tests := []struct {
		raw      string
		expected time.Duration
	}{
		{"", 0},
		{"   ", 0},
		{"10s", 10 * time.Second},
		{"8s", 8 * time.Second},
		{"15", 15 * time.Second},
		{"500ms", 500 * time.Millisecond},
		{"0", 0},
		{"0s", 0},
		{"0m", 0},
		{"0ms", 0},
		{"none", 0},
		{"NONE", 0},
		{"unlimited", 0},
		{"disabled", 0},
		{"off", 0},
		{"never", 0},
		{"invalid", 0},
		{"-5s", 0},
		{"-1", 0},
		{"9223372037", 0},
		{"18446744074", 0},
		{"36028797018963968", 0},
	}
	for _, tt := range tests {
		cfg := &config.CodexConfig{StreamBootstrapTimeout: tt.raw}
		if got := cfg.StreamBootstrapTimeoutDuration(); got != tt.expected {
			t.Errorf("StreamBootstrapTimeoutDuration(%q) = %v, want %v", tt.raw, got, tt.expected)
		}
	}
	var nilCfg *config.CodexConfig
	if got := nilCfg.StreamBootstrapTimeoutDuration(); got != 0 {
		t.Errorf("nil StreamBootstrapTimeoutDuration = %v, want 0", got)
	}
}

func TestCodexExecutor_BootstrapBuffering_TimeBudgetReleasesStream(t *testing.T) {
	t0 := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock := withMockClock(t, t0)

	bootstrapStarted := make(chan struct{})
	var once sync.Once
	cleanup := setCodexBootstrapNowForTest(func() time.Time {
		now := clock.now()
		once.Do(func() {
			close(bootstrapStarted)
		})
		return now
	})
	t.Cleanup(cleanup)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.in_progress\ndata: " + codexInProgressEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		<-bootstrapStarted
		clock.advance(11 * time.Second)

		_, _ = w.Write([]byte("event: response.in_progress\ndata: " + codexInProgressEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		_, _ = w.Write([]byte("event: error\ndata: " + codexOverloadEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfigWithTimeout(true, "10s")).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("the time budget must release the stream before the overload arrives: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result once the time budget released the stream")
	}
	if _, streamErr := drainChunks(result); streamErr == nil {
		t.Fatal("expected the overload to arrive in-stream after the time budget released")
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_TimeBudgetReleasesStream(t *testing.T) {
	t0 := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock := withMockClock(t, t0)

	bootstrapStarted := make(chan struct{})
	var once sync.Once
	cleanup := setCodexBootstrapNowForTest(func() time.Time {
		now := clock.now()
		once.Do(func() {
			close(bootstrapStarted)
		})
		return now
	})
	t.Cleanup(cleanup)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, errRead := conn.ReadMessage(); errRead != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(codexInProgressEvent))

		<-bootstrapStarted

		clock.advance(11 * time.Second)

		_ = conn.WriteMessage(websocket.TextMessage, []byte(codexInProgressEvent))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(codexOverloadEvent))
	}))
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfigWithTimeout(true, "10s")).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("the time budget must release the stream before the overload arrives: %v", err)
	}
	if result == nil {
		t.Fatal("expected a stream result once the time budget released the stream")
	}
	if _, streamErr := drainChunks(result); streamErr == nil {
		t.Fatal("expected the overload to arrive in-stream after the time budget released")
	}
}

func TestCodexExecutor_BootstrapBuffering_DisabledTimeBudget(t *testing.T) {
	t0 := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock := withMockClock(t, t0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.in_progress\ndata: " + codexInProgressEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		clock.advance(100 * time.Second)

		_, _ = w.Write([]byte("event: response.in_progress\ndata: " + codexInProgressEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		_, _ = w.Write([]byte("event: error\ndata: " + codexOverloadEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	req, opts := codexTestRequest()
	cfg := codexBufferingConfigWithTimeout(true, "none")
	result, err := NewCodexExecutor(cfg).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err == nil {
		t.Fatal("expected failover error when time budget is disabled")
	}
	if result != nil {
		t.Fatal("expected nil stream result on failover")
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_DisabledTimeBudget(t *testing.T) {
	t0 := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock := withMockClock(t, t0)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, errRead := conn.ReadMessage(); errRead != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(codexInProgressEvent))

		clock.advance(100 * time.Second)

		_ = conn.WriteMessage(websocket.TextMessage, []byte(codexInProgressEvent))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(codexOverloadEvent))
	}))
	defer server.Close()

	req, opts := codexWebsocketRequest()
	cfg := codexBufferingConfigWithTimeout(true, "none")
	result, err := NewCodexWebsocketsExecutor(cfg).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err == nil {
		t.Fatal("expected failover error when time budget is disabled")
	}
	if result != nil {
		t.Fatal("expected nil stream result on failover")
	}
}

func TestCodexExecutor_BootstrapBuffering_DefaultUnsetTimeoutIsUnlimited(t *testing.T) {
	t0 := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock := withMockClock(t, t0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.in_progress\ndata: " + codexInProgressEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		clock.advance(100 * time.Second)

		_, _ = w.Write([]byte("event: response.in_progress\ndata: " + codexInProgressEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		_, _ = w.Write([]byte("event: error\ndata: " + codexOverloadEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	req, opts := codexTestRequest()

	cfg := codexBufferingConfig(true)
	result, err := NewCodexExecutor(cfg).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err == nil {
		t.Fatal("expected failover error when default timeout is unlimited")
	}
	if result != nil {
		t.Fatal("expected nil stream result on failover")
	}
}

func TestCodexExecutor_BootstrapBuffering_OverloadDirectlyAfterTimeoutDeliveredInStream(t *testing.T) {
	t0 := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock := withMockClock(t, t0)

	bootstrapStarted := make(chan struct{})
	var once sync.Once
	cleanup := setCodexBootstrapNowForTest(func() time.Time {
		now := clock.now()
		once.Do(func() {
			close(bootstrapStarted)
		})
		return now
	})
	t.Cleanup(cleanup)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		<-bootstrapStarted

		clock.advance(11 * time.Second)

		_, _ = w.Write([]byte("event: error\ndata: " + codexOverloadEvent + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(codexBufferingConfigWithTimeout(true, "10s")).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("expected stream result without failover when overload arrives after timeout: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil stream result")
	}
	if _, streamErr := drainChunks(result); streamErr == nil {
		t.Fatal("expected overload error to be delivered in-stream")
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_OverloadDirectlyAfterTimeoutDeliveredInStream(t *testing.T) {
	t0 := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock := withMockClock(t, t0)

	bootstrapStarted := make(chan struct{})
	var once sync.Once
	cleanup := setCodexBootstrapNowForTest(func() time.Time {
		now := clock.now()
		once.Do(func() {
			close(bootstrapStarted)
		})
		return now
	})
	t.Cleanup(cleanup)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, errRead := conn.ReadMessage(); errRead != nil {
			return
		}

		<-bootstrapStarted

		clock.advance(11 * time.Second)

		_ = conn.WriteMessage(websocket.TextMessage, []byte(codexOverloadEvent))
	}))
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfigWithTimeout(true, "10s")).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("expected stream result without failover when overload arrives after timeout: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil stream result")
	}
	if _, streamErr := drainChunks(result); streamErr == nil {
		t.Fatal("expected overload error to be delivered in-stream")
	}
}

func TestCodexWebsocketsExecutor_BootstrapBuffering_StatusBearingErrorAfterTimeoutDeliveredInStream(t *testing.T) {
	t0 := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock := withMockClock(t, t0)

	bootstrapStarted := make(chan struct{})
	var once sync.Once
	cleanup := setCodexBootstrapNowForTest(func() time.Time {
		now := clock.now()
		once.Do(func() {
			close(bootstrapStarted)
		})
		return now
	})
	t.Cleanup(cleanup)

	statusBearingError := `{"type":"error","status":429,"error":{"message":"Rate limit exceeded","type":"requests","code":"rate_limit_exceeded"}}`

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, errRead := conn.ReadMessage(); errRead != nil {
			return
		}

		<-bootstrapStarted

		clock.advance(11 * time.Second)

		_ = conn.WriteMessage(websocket.TextMessage, []byte(statusBearingError))
	}))
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(codexBufferingConfigWithTimeout(true, "10s")).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("expected stream result without failover when status-bearing error arrives after timeout: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil stream result")
	}
	if _, streamErr := drainChunks(result); streamErr == nil {
		t.Fatal("expected status-bearing error to be delivered in-stream")
	}
}

func TestCodexWebsocketsExecutor_UnbufferedUpstreamErrorIsDeliveredBeforeDisconnectNotify(t *testing.T) {
	frames := map[string]string{
		"error frame":     `{"type":"error","status":429,"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","resets_in_seconds":600}}`,
		"response.failed": `{"type":"response.failed","response":{"error":{"type":"usage_limit_reached","status_code":429,"message":"The usage limit has been reached","resets_in_seconds":600}}}`,
	}
	for name, frame := range frames {
		t.Run(name, func(t *testing.T) {
			server := codexWebsocketServerHoldingConnection(t, codexCreatedEvent, codexOutputDeltaEvent, frame)
			defer server.Close()

			exec := NewCodexWebsocketsExecutor(codexBufferingConfig(false))
			exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}

			const sessionID = "issue-6503-session"
			disconnectCh := exec.UpstreamDisconnectChan(sessionID)
			if disconnectCh == nil {
				t.Fatal("expected a disconnect channel")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			canceled := make(chan struct{})
			go func() {
				<-disconnectCh
				cancel()
				close(canceled)
			}()

			req, opts := codexWebsocketRequest()
			opts.Metadata = map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: sessionID}
			result, err := exec.ExecuteStream(ctx, codexTestAuth(server.URL), req, opts)
			if err != nil {
				t.Fatalf("ExecuteStream() error = %v", err)
			}

			select {
			case <-canceled:
			case <-time.After(200 * time.Millisecond):
			}

			var streamErr error
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					streamErr = chunk.Err
				}
			}
			if streamErr == nil {
				t.Fatal("the upstream usage-limit error was dropped by the disconnect-triggered cancellation")
			}
			if got := statusCodeFromTestError(t, streamErr); got != http.StatusTooManyRequests {
				t.Fatalf("status code = %d, want %d", got, http.StatusTooManyRequests)
			}
			select {
			case <-canceled:
			case <-time.After(5 * time.Second):
				t.Fatal("downstream disconnect was never signalled after the error was delivered")
			}
		})
	}
}

func TestCodexWebsocketsExecutor_PreOutputUpstreamErrorDoesNotNotifyDisconnect(t *testing.T) {
	frames := map[string]string{
		"error frame":     `{"type":"error","status":429,"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","resets_in_seconds":600}}`,
		"response.failed": `{"type":"response.failed","response":{"error":{"type":"usage_limit_reached","status_code":429,"message":"The usage limit has been reached","resets_in_seconds":600}}}`,
	}
	cases := []struct {
		label     string
		buffering bool
		preamble  []string
	}{
		{label: "immediate/buffering=false", buffering: false},
		{label: "immediate/buffering=true", buffering: true},
		{label: "after_handshake/buffering=true", buffering: true, preamble: []string{codexCreatedEvent, codexInProgressEvent}},
	}
	for name, frame := range frames {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%s", name, tc.label), func(t *testing.T) {
				seq := append(append([]string(nil), tc.preamble...), frame)
				server := codexWebsocketServerHoldingConnection(t, seq...)
				defer server.Close()

				exec := NewCodexWebsocketsExecutor(codexBufferingConfig(tc.buffering))
				exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}

				const sessionID = "issue-7051-session"
				disconnectCh := exec.UpstreamDisconnectChan(sessionID)
				if disconnectCh == nil {
					t.Fatal("expected a disconnect channel")
				}

				req, opts := codexWebsocketRequest()
				opts.Metadata = map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: sessionID}
				result, err := exec.ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)

				streamErr := err
				if result != nil {
					for chunk := range result.Chunks {
						if chunk.Err != nil {
							streamErr = chunk.Err
						}
					}
				}
				if streamErr == nil {
					t.Fatal("expected upstream usage-limit error")
				}
				if got := statusCodeFromTestError(t, streamErr); got != http.StatusTooManyRequests {
					t.Fatalf("status code = %d, want %d", got, http.StatusTooManyRequests)
				}
				select {
				case disconnectErr := <-disconnectCh:
					t.Fatalf("pre-output upstream error must not notify downstream disconnect, got %v", disconnectErr)
				default:
				}
			})
		}
	}
}
