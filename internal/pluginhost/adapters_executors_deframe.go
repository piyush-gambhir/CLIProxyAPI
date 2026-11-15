package pluginhost

import (
	"bytes"
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)





























type executorSSEDeframer struct {
	buffer []byte
	sawSSE bool
}

func (d *executorSSEDeframer) feed(payload []byte) [][]byte {
	if len(payload) == 0 {
		return nil
	}




	if len(d.buffer) == 0 && !d.sawSSE && !canBeginSSEEvent(payload) {
		return [][]byte{payload}
	}
	d.buffer = append(d.buffer, payload...)

	var chunks [][]byte
	for {
		if len(d.buffer) == 0 {
			break
		}




		dropBlankSSELine := false
		if idx := bytes.IndexByte(d.buffer, '\n'); idx >= 0 && isBlankSSELine(d.buffer[:idx]) {
			d.advance(idx + 1)
			dropBlankSSELine = true
		}
		if dropBlankSSELine {
			continue
		}
		if len(d.buffer) == 0 {
			break
		}
		if !sseDataLinePrefix(d.buffer) && !bytes.HasPrefix(d.buffer, []byte("event:")) && !bytes.HasPrefix(d.buffer, []byte(":")) {






			if isSSEFieldPrefixPartial(d.buffer) {
				break
			}
			if d.sawSSE {
				idx := bytes.IndexByte(d.buffer, '\n')
				if idx < 0 {
					break
				}
				chunks = append(chunks, bytes.Clone(d.buffer[:idx+1]))
				d.advance(idx + 1)
				continue
			}
			chunks = append(chunks, bytes.Clone(d.buffer))
			d.buffer = nil
			break
		}
		event, complete := d.takeEvent()
		if !complete {
			break
		}
		d.sawSSE = true
		if len(event) > 0 {
			chunks = append(chunks, event)
		}
	}
	return chunks
}


func (d *executorSSEDeframer) flush() []byte {
	pending := d.buffer
	d.buffer = nil




	pending = bytes.TrimRight(pending, "\r\n")
	if len(pending) == 0 {
		return nil
	}
	return pending
}






func canBeginSSEEvent(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	switch b[0] {
	case ':':
		return true
	case 'd':
		return sseFieldPrefixCandidate(b, "data:")
	case 'e':
		return sseFieldPrefixCandidate(b, "event:")
	}
	return false
}

func sseFieldPrefixCandidate(b []byte, field string) bool {
	if len(b) <= len(field) {
		return bytes.HasPrefix([]byte(field), b)
	}
	return bytes.HasPrefix(b, []byte(field))
}




func isSSEFieldPrefixPartial(b []byte) bool {
	if len(b) == 0 || len(b) >= len("event:") {
		return false
	}
	return bytes.HasPrefix([]byte("data:"), b) || bytes.HasPrefix([]byte("event:"), b)
}




func sseDataLinePrefix(b []byte) bool {
	return bytes.HasPrefix(b, []byte("data:"))
}





func (d *executorSSEDeframer) takeEvent() ([]byte, bool) {
	if len(d.buffer) == 0 {
		return nil, false
	}
	idx := indexSSEFrameBoundary(d.buffer)
	if idx < 0 {
		return nil, false
	}
	end := idx + 1
	event := bytes.Clone(d.buffer[:end])
	d.advance(end)
	if isSSECommentOnlyEvent(event) {
		return nil, true
	}
	return event, true
}

func (d *executorSSEDeframer) advance(n int) {
	rest := d.buffer[n:]
	if len(rest) == 0 {
		d.buffer = nil
		return
	}
	d.buffer = append(d.buffer[:0], rest...)
}











func deframeExecutorSSEChunks(ctx context.Context, in <-chan pluginapi.ExecutorStreamChunk) <-chan pluginapi.ExecutorStreamChunk {
	if in == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out := make(chan pluginapi.ExecutorStreamChunk)
	go func() {
		defer close(out)
		deframer := &executorSSEDeframer{}

		emit := func(payloads [][]byte) bool {
			for _, payload := range payloads {
				if len(payload) == 0 {
					continue
				}
				if !sendExecutorPluginStreamChunk(ctx, out, pluginapi.ExecutorStreamChunk{Payload: payload}) {
					return false
				}
			}
			return true
		}

		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-in:
				if !ok {
					if tail := deframer.flush(); len(tail) > 0 {
						_ = emit([][]byte{tail})
					}
					return
				}
				if chunk.Err != nil {


					if tail := deframer.flush(); len(tail) > 0 {
						if !emit([][]byte{tail}) {
							return
						}
					}
					_ = sendExecutorPluginStreamChunk(ctx, out, chunk)
					continue
				}
				if !emit(deframer.feed(chunk.Payload)) {
					return
				}
			}
		}
	}()
	return out
}





func indexSSEFrameBoundary(b []byte) int {
	pos := 0
	for {
		idx := bytes.IndexByte(b[pos:], '\n')
		if idx < 0 {
			return -1
		}
		lineEnd := pos + idx
		if isBlankSSELine(b[pos:lineEnd]) {
			return lineEnd
		}
		pos = lineEnd + 1
	}
}

func isBlankSSELine(line []byte) bool {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	return len(line) == 0
}

func isSSECommentOnlyEvent(event []byte) bool {
	sawComment := false
	for _, line := range bytes.Split(event, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if !bytes.HasPrefix(trimmed, []byte(":")) {
			return false
		}
		sawComment = true
	}
	return sawComment
}
