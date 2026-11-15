package common

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

type UnsupportedPartError struct {
	Type string
}

func (e *UnsupportedPartError) Error() string {
	if e == nil || e.Type == "" {
		return "unsupported content part"
	}
	return fmt.Sprintf("unsupported content part: %s", e.Type)
}

func (e *UnsupportedPartError) StatusCode() int { return 400 }

func (e *UnsupportedPartError) IsRequestScoped() bool { return true }

type UserTurnDrops struct {
	turn  string
	first string
}

func (d *UserTurnDrops) Drop(partType string) {
	if d.turn == "" {
		d.turn = partType
	}
}

func (d *UserTurnDrops) EndTurn(sendable int) {
	if d.turn != "" && sendable <= 0 && d.first == "" {
		d.first = d.turn
	}
	d.turn = ""
}

func (d *UserTurnDrops) Err() error {
	if d.first == "" {
		return nil
	}
	return &UnsupportedPartError{Type: d.first}
}

type UserRun struct {
	drops    UserTurnDrops
	sendable int
}

func (r *UserRun) Add() {
	if r != nil {
		r.sendable++
	}
}

func (r *UserRun) Drop(partType string) {
	if r != nil {
		r.drops.Drop(partType)
	}
}

func (r *UserRun) End() {
	if r != nil {
		r.drops.EndTurn(r.sendable)
		r.sendable = 0
	}
}

func (r *UserRun) Err() error {
	if r == nil {
		return nil
	}
	return r.drops.Err()
}

func IsInteractionsInstructionStep(step ojson.Result, inherited bool) bool {
	name := strings.ToLower(strings.TrimSpace(step.Get("role").String()))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(step.Get("type").String()))
	}
	switch name {
	case "developer", "system":
		return true
	case "user", "assistant", "model", "model_output", "thought":
		return false
	}
	return inherited
}

func InteractionsAttachmentType(part ojson.Result) string {
	if !part.IsObject() {
		return ""
	}
	if partType := strings.ToLower(strings.TrimSpace(part.Get("type").String())); partType != "" {
		if partType == "text" {
			return ""
		}
		return partType
	}
	if part.Get("inlineData").Exists() || part.Get("inline_data").Exists() {
		return "inlineData"
	}
	if part.Get("fileData").Exists() || part.Get("file_data").Exists() {
		return "fileData"
	}
	return ""
}

func GeminiPartIsSendable(part []byte) bool {
	parsed := util.ParseGJSONBytesNoCopy(part)
	text := parsed.Get("text")
	if !text.Exists() || strings.TrimSpace(text.String()) != "" {
		return true
	}
	for _, key := range []string{"functionCall", "functionResponse", "inlineData", "inline_data", "fileData", "file_data"} {
		if parsed.Get(key).Exists() {
			return true
		}
	}
	return false
}

func CountSendableGeminiParts(parts [][]byte) int {
	count := 0
	for _, part := range parts {
		if GeminiPartIsSendable(part) {
			count++
		}
	}
	return count
}

func IsHTTPURL(value string) bool {
	parsed, errParse := url.Parse(strings.TrimSpace(value))
	if errParse != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
}
