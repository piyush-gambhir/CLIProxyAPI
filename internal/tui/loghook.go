package tui

import (
	"fmt"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)


type LogHook struct {
	ch        chan string
	formatter log.Formatter
	mu        sync.Mutex
	levels    []log.Level
}


func NewLogHook(bufSize int) *LogHook {
	return &LogHook{
		ch:        make(chan string, bufSize),
		formatter: &log.TextFormatter{DisableColors: true, FullTimestamp: true},
		levels:    log.AllLevels,
	}
}


func (h *LogHook) SetFormatter(f log.Formatter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.formatter = f
}


func (h *LogHook) Levels() []log.Level {
	return h.levels
}


func (h *LogHook) Fire(entry *log.Entry) error {
	h.mu.Lock()
	f := h.formatter
	h.mu.Unlock()

	var line string
	if f != nil {
		b, err := f.Format(entry)
		if err == nil {
			line = strings.TrimRight(string(b), "\n\r")
		} else {
			line = fmt.Sprintf("[%s] %s", entry.Level, entry.Message)
		}
	} else {
		line = fmt.Sprintf("[%s] %s", entry.Level, entry.Message)
	}


	select {
	case h.ch <- line:
	default:

		select {
		case <-h.ch:
		default:
		}
		select {
		case h.ch <- line:
		default:
		}
	}
	return nil
}


func (h *LogHook) Chan() <-chan string {
	return h.ch
}
