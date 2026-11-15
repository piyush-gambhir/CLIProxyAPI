package openai

import (
	"testing"
)

func TestResponsesLocalInterruptRetainsClaimedFrameAfterEnd(t *testing.T) {
	local := newResponsesLocalInterrupt()
	local.begin(false)
	if !local.deliver([]byte(`{"type":"response.interrupt","response_id":"unknown"}`)) {
		t.Fatal("frame was not claimed while active")
	}
	local.deactivate()
	local.end()
	if local.deliver([]byte(`{"type":"response.interrupt","response_id":"unknown-2"}`)) {
		t.Fatal("frame claimed after end")
	}
	select {
	case frame := <-local.framesChan():
		if got := string(frame); got != `{"type":"response.interrupt","response_id":"unknown"}` {
			t.Fatalf("retained frame = %s", got)
		}
	default:
		t.Fatal("claimed frame was silently dropped by end()")
	}

	select {
	case frame := <-local.framesChan():
		t.Fatalf("unexpected second frame: %s", frame)
	default:
	}
}

func TestResponsesLocalInterruptRejectsWhenQueueFull(t *testing.T) {
	local := newResponsesLocalInterrupt()
	local.begin(false)
	if !local.deliver([]byte(`{"type":"response.interrupt","response_id":"first"}`)) {
		t.Fatal("first frame was not claimed")
	}
	if local.deliver([]byte(`{"type":"response.interrupt","response_id":"second"}`)) {
		t.Fatal("second frame claimed while queue was full")
	}
	frame := local.takeClaimed()
	if got := string(frame); got != `{"type":"response.interrupt","response_id":"first"}` {
		t.Fatalf("claimed frame = %s", got)
	}
	if frame := local.takeClaimed(); frame != nil {
		t.Fatalf("unexpected second claimed frame: %s", frame)
	}
}

func TestResponsesLocalInterruptWebsocketOwnershipIsAtomic(t *testing.T) {
	local := newResponsesLocalInterrupt()
	local.begin(true)
	if local.deliver([]byte(`{"type":"response.interrupt","response_id":"r1"}`)) {
		t.Fatal("frame claimed for a websocket-owned turn")
	}
	local.endWebsocketTurn()
	local.begin(false)
	if !local.deliver([]byte(`{"type":"response.interrupt","response_id":"r1"}`)) {
		t.Fatal("frame not claimed for an HTTP turn")
	}
}
