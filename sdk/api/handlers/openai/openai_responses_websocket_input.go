package openai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)










type responsesLocalInterrupt struct {
	mu           sync.Mutex
	active       bool
	wsTurnActive bool
	frames       chan []byte
}

func newResponsesLocalInterrupt() *responsesLocalInterrupt {
	return &responsesLocalInterrupt{frames: make(chan []byte, 1)}
}




func (s *responsesLocalInterrupt) begin(websocketOwned bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.active = true
	s.wsTurnActive = websocketOwned
	s.mu.Unlock()
}



func (s *responsesLocalInterrupt) reactivate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.active = true
	s.mu.Unlock()
}

func (s *responsesLocalInterrupt) endWebsocketTurn() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.wsTurnActive = false
	s.mu.Unlock()
}




func (s *responsesLocalInterrupt) stopClaims() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
}



func (s *responsesLocalInterrupt) end() {
	s.stopClaims()
}


func (s *responsesLocalInterrupt) deactivate() {
	s.stopClaims()
}


func (s *responsesLocalInterrupt) isActive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}




func (s *responsesLocalInterrupt) deliver(payload []byte) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.wsTurnActive {
		return false
	}
	select {
	case s.frames <- bytes.Clone(payload):
		return true
	default:
		return false
	}
}

func (s *responsesLocalInterrupt) framesChan() <-chan []byte {
	if s == nil {
		return nil
	}
	return s.frames
}



func (s *responsesLocalInterrupt) takeClaimed() []byte {
	if s == nil {
		return nil
	}
	select {
	case frame := <-s.frames:
		return frame
	default:
		return nil
	}
}




func readResponsesWebsocketInput(ctx context.Context, cancel context.CancelCauseFunc, conn *websocket.Conn, interrupt func([]byte) error, local *responsesLocalInterrupt, writer *responsesWebsocketWriter, timeline websocketTimelineAppender, epoch *atomic.Uint64) (<-chan cliproxyexecutor.WebsocketInput, <-chan struct{}) {
	input := make(chan cliproxyexecutor.WebsocketInput, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(input)
		for {
			kind, payload, errRead := conn.ReadMessage()
			if errRead != nil {
				cancel(errRead)
				return
			}
			msgEpoch := epoch.Load()
			if kind != websocket.TextMessage && kind != websocket.BinaryMessage {
				continue
			}


			if json.Valid(payload) && ojson.GetBytesNoCopy(payload, "type").String() == "response.interrupt" {
				appendResponsesInterruptDiagnostic(timeline, payload, "")
				errInterrupt := interrupt(payload)
				switch {
				case errInterrupt == nil:
					appendResponsesInterruptDiagnostic(timeline, payload, "handled")
					continue
				case errors.Is(errInterrupt, cliproxyexecutor.ErrNoActiveUpstreamWebsocket) && local.deliver(payload):
					appendResponsesInterruptDiagnostic(timeline, payload, "local_http")
					continue
				default:
					appendResponsesInterruptDiagnostic(timeline, payload, "rejected")


					_, errWrite := writeResponsesWebsocketError(writer, nil, &interfaces.ErrorMessage{
						StatusCode: http.StatusBadRequest,
						Error:      errInterrupt,
					})
					if errWrite != nil {
						cancel(errWrite)
						return
					}
					continue
				}
			}
			message := cliproxyexecutor.WebsocketInput{Payload: payload, Epoch: msgEpoch}
			select {
			case input <- message:
			case <-ctx.Done():
				return
			}
		}
	}()
	return input, done
}

func appendResponsesInterruptDiagnostic(timeline websocketTimelineAppender, payload []byte, outcome string) {
	if timeline == nil {
		return
	}


	idHash := sha256.Sum256([]byte(ojson.GetBytesNoCopy(payload, "response_id").String()))
	diagnostic := struct {
		Type           string `json:"type"`
		ResponseIDHash string `json:"response_id_hash"`
		Outcome        string `json:"outcome,omitempty"`
	}{Type: "response.interrupt", ResponseIDHash: fmt.Sprintf("%x", idHash[:8]), Outcome: outcome}
	data, errMarshal := json.Marshal(diagnostic)
	if errMarshal != nil {
		return
	}
	event := "interrupt"
	if outcome != "" {
		event = "interrupt.outcome"
	}
	timeline.Append(event, data, time.Now())
}

func (h *OpenAIResponsesAPIHandler) forwardResponsesWebsocketInterrupt(ctx context.Context, sessionID string, payload []byte) error {
	responseID := ojson.GetBytesNoCopy(payload, "response_id")
	if responseID.Type != ojson.String || strings.TrimSpace(responseID.String()) == "" {
		return fmt.Errorf("response.interrupt requires response_id")
	}
	if h == nil || h.AuthManager == nil {
		return cliproxyexecutor.ErrNoActiveUpstreamWebsocket
	}
	exec, ok := h.AuthManager.Executor("codex")
	if !ok || exec == nil {
		return cliproxyexecutor.ErrNoActiveUpstreamWebsocket
	}
	sender, ok := exec.(interface {
		InterruptExecutionSession(context.Context, string, []byte) error
	})
	if !ok || sender == nil {
		return fmt.Errorf("upstream executor does not support response.interrupt")
	}
	ctx = cliproxyexecutor.WithWebsocketAuthCheck(ctx, func(authID string) bool {

		auth, found := h.AuthManager.GetByID(authID)
		if !found || auth == nil {
			auth, found = h.AuthManager.GetExecutionSessionAuthByID(sessionID, authID)
		}
		return found && auth != nil && !auth.Disabled && auth.Status != coreauth.StatusDisabled
	})
	return sender.InterruptExecutionSession(ctx, sessionID, payload)
}
