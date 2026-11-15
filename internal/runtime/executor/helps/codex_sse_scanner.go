package helps

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

const codexSSEDiagnosticLines = 8

var codexSSEDataPrefix = []byte("data:")

type codexSSEQueuedItem struct {
	payload  []byte
	rawLines int
	rawBytes int
}

type CodexSSEScanner struct {
	ctx      context.Context
	lines    *bufio.Scanner
	maxBytes int

	stock bool

	held    []byte
	count   int
	size    int
	joined  []byte
	nesting codexJSONNesting
	queue   []codexSSEQueuedItem
	current codexSSEQueuedItem

	warned      bool
	onLine      func(line []byte)
	budgetCheck func(line []byte, heldLines int, heldBytes int) bool
}

func NewCodexSSEScanner(ctx context.Context, r io.Reader, maxBytes int) *CodexSSEScanner {
	lines := bufio.NewScanner(r)
	lines.Buffer(nil, maxBytes)
	return &CodexSSEScanner{ctx: ctx, lines: lines, maxBytes: maxBytes}
}

func (s *CodexSSEScanner) SetOnLine(fn func(line []byte)) {
	s.onLine = fn
}

func (s *CodexSSEScanner) HeldLines() int {
	return s.count
}

func (s *CodexSSEScanner) HeldBytes() int {
	return s.size
}

func (s *CodexSSEScanner) SetBudgetChecker(fn func(line []byte, heldLines int, heldBytes int) bool) {
	s.budgetCheck = fn
}

func (s *CodexSSEScanner) SetAssembly(on bool) { s.stock = !on }

func (s *CodexSSEScanner) Scan() bool {
	if len(s.queue) > 0 {
		s.current, s.queue = s.queue[0], s.queue[1:]
		return true
	}
	for s.lines.Scan() {
		line := s.lines.Bytes()
		if s.onLine != nil {
			s.onLine(line)
		}
		if !bytes.HasPrefix(line, codexSSEDataPrefix) {
			if len(line) == 0 && s.count > 0 {
				s.release([]byte{})
				return s.Scan()
			}
			s.current = codexSSEQueuedItem{payload: line, rawLines: 1, rawBytes: len(line)}
			return true
		}
		if s.count == 0 {
			if s.stock || !codexSSEIncompleteJSON(bytes.TrimSpace(line[len(codexSSEDataPrefix):])) {
				s.current = codexSSEQueuedItem{payload: line, rawLines: 1, rawBytes: len(line)}
				return true
			}
		} else if s.size+len(line) > s.maxBytes {

			s.release(bytes.Clone(line))
			return s.Scan()
		}

		closed := s.hold(line)
		if !closed && s.budgetCheck != nil && s.budgetCheck(line, s.count, s.size) {
			s.budgetCheck = nil
			s.current = codexSSEQueuedItem{payload: nil, rawLines: 0, rawBytes: 0}
			return true
		}
		if closed {
			s.release()
			return s.Scan()
		}
	}
	if s.count > 0 {
		s.release()
		return s.Scan()
	}
	return false
}

func (s *CodexSSEScanner) hold(line []byte) bool {
	value := codexSSEValue(line)
	if s.count > 0 {
		s.held = append(s.held, '\n')
		s.joined = append(s.joined, '\n')
		s.nesting.feed([]byte{'\n'})
	}
	s.held = append(s.held, line...)
	s.joined = append(s.joined, value...)
	s.count++
	s.size += len(line)
	return s.nesting.feed(value)
}

func (s *CodexSSEScanner) release(end ...[]byte) {
	assembled := s.assemble(s.held, s.count, s.joined)
	s.queue = append(s.queue, codexSSEQueuedItem{
		payload:  assembled,
		rawLines: s.count,
		rawBytes: s.size,
	})
	for _, e := range end {
		s.queue = append(s.queue, codexSSEQueuedItem{
			payload:  e,
			rawLines: 1,
			rawBytes: len(e),
		})
	}
	s.held, s.count, s.size, s.joined, s.nesting = nil, 0, 0, nil, codexJSONNesting{}
}

func codexSSEValue(line []byte) []byte {
	return bytes.TrimPrefix(line[len(codexSSEDataPrefix):], []byte(" "))
}

func codexSSEClosed(joined []byte) bool {
	trimmed := bytes.TrimRight(joined, " \t\r\n")
	if len(trimmed) == 0 || (trimmed[len(trimmed)-1] != '}' && trimmed[len(trimmed)-1] != ']') {
		return false
	}
	return json.Valid(joined)
}

func codexSSEIncompleteJSON(value []byte) bool {
	if len(value) == 0 || (value[0] != '{' && value[0] != '[') || bytes.Equal(value, []byte("[DONE]")) {
		return false
	}
	return !codexSSEClosed(value)
}

type codexJSONNesting struct {
	depth    int
	inString bool
	escaped  bool
}

func (n *codexJSONNesting) feed(p []byte) bool {
	for _, c := range p {
		switch {
		case n.inString:
			switch {
			case n.escaped:
				n.escaped = false
			case c == '\\':
				n.escaped = true
			case c == '"':
				n.inString = false
			}
		case c == '"':
			n.inString = true
		case c == '{' || c == '[':
			n.depth++
		case c == '}' || c == ']':
			n.depth--
			if n.depth <= 0 {
				return true
			}
		}
	}
	return false
}

func (s *CodexSSEScanner) Bytes() []byte { return s.current.payload }

func (s *CodexSSEScanner) RawLines() int {
	if s.current.rawLines == 0 {
		return 1
	}
	return s.current.rawLines
}

func (s *CodexSSEScanner) RawBytes() int {
	if s.current.rawBytes == 0 {
		return len(s.current.payload)
	}
	return s.current.rawBytes
}

func (s *CodexSSEScanner) Err() error { return s.lines.Err() }

func (s *CodexSSEScanner) assemble(held []byte, lines int, joined []byte) []byte {
	if lines == 1 {
		s.diagnose(held, lines, bytes.TrimSpace(joined))
		return held
	}
	if json.Valid(joined) {
		var compacted bytes.Buffer
		compacted.Grow(len(codexSSEDataPrefix) + 1 + len(joined))
		compacted.Write(codexSSEDataPrefix)
		compacted.WriteByte(' ')
		if errCompact := json.Compact(&compacted, joined); errCompact == nil {
			return compacted.Bytes()
		}
	}
	s.diagnose(held, lines, joined)
	return held
}

func (s *CodexSSEScanner) diagnose(held []byte, lines int, data []byte) {
	if s.warned || len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) || json.Valid(data) {
		return
	}
	s.warned = true
	offset := int64(-1)
	var syntaxErr *json.SyntaxError
	if errors.As(json.Unmarshal(data, new(json.RawMessage)), &syntaxErr) {
		offset = syntaxErr.Offset
	}
	shown := min(lines, codexSSEDiagnosticLines)
	lengths := make([]string, shown)
	valid := make([]string, shown)
	for i := range shown {
		var line []byte
		line, held, _ = bytes.Cut(held, []byte("\n"))
		value := bytes.TrimSpace(line[len(codexSSEDataPrefix):])
		lengths[i] = strconv.Itoa(len(value))
		valid[i] = strconv.FormatBool(json.Valid(value))
	}
	more := ""
	if lines > shown {
		more = ",+" + strconv.Itoa(lines-shown)
	}
	LogWithRequestID(s.ctx).WithFields(log.Fields{
		"data_lines":        lines,
		"line_bytes":        strings.Join(lengths, ",") + more,
		"lines_valid":       strings.Join(valid, ",") + more,
		"data_bytes":        len(data),
		"json_error_offset": offset,
	}).Warn("codex executor: upstream SSE event data is not valid JSON")
}
