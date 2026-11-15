package helps

import (
	"bytes"

	"github.com/MoeForge/moego/ojson"
)




func EnsureResponsesUsageDetails(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}

	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return payload
	}


	if trimmed[0] == '{' {
		if ojson.GetBytesNoCopy(trimmed, "object").String() == "response.compaction" {
			return payload
		}
		doc := ojson.NewDoc(trimmed)
		if !EnsureResponsesUsageDetailsDoc(doc) {
			return payload
		}
		return doc.Bytes()
	}


	if bytes.Contains(payload, []byte("data:")) {
		lines := bytes.Split(payload, []byte("\n"))
		modified := false
		for i, line := range lines {
			trimmedLine := bytes.TrimSpace(line)
			if !bytes.HasPrefix(trimmedLine, []byte("data:")) {
				continue
			}
			prefixLen := len("data:")
			if bytes.HasPrefix(line, []byte("data: ")) {
				prefixLen = len("data: ")
			} else if bytes.HasPrefix(line, []byte("data:")) {
				prefixLen = len("data:")
			}
			dataPayload := bytes.TrimSpace(line[prefixLen:])
			if len(dataPayload) == 0 || dataPayload[0] != '{' {
				continue
			}
			if ojson.GetBytesNoCopy(dataPayload, "object").String() == "response.compaction" {
				continue
			}
			doc := ojson.NewDoc(dataPayload)
			if EnsureResponsesUsageDetailsDoc(doc) {
				newPrefix := bytes.Clone(line[:prefixLen])
				lines[i] = append(newPrefix, doc.Bytes()...)
				modified = true
			}
		}
		if modified {
			return bytes.Join(lines, []byte("\n"))
		}
		return payload
	}

	return payload
}



func EnsureResponsesUsageDetailsDoc(doc *ojson.Doc) bool {
	if doc == nil || doc.Len() == 0 {
		return false
	}
	if doc.Get("object").String() == "response.compaction" {
		return false
	}
	changed1 := ensureUsageDetailsAtDoc(doc, "response.usage")
	changed2 := ensureUsageDetailsAtDoc(doc, "usage")
	return changed1 || changed2
}

func ensureUsageDetailsAtDoc(doc *ojson.Doc, path string) bool {
	usageNode := doc.Get(path)
	if !usageNode.Exists() || !usageNode.IsObject() {
		return false
	}

	changed := false
	outputDetails := usageNode.Get("output_tokens_details")
	if !outputDetails.Exists() {
		if errSet := doc.Set(path+".output_tokens_details.reasoning_tokens", 0); errSet == nil {
			changed = true
		}
	} else if outputDetails.Type == ojson.Null || !outputDetails.IsObject() {
		if errSet := doc.SetRaw(path+".output_tokens_details", `{"reasoning_tokens":0}`); errSet == nil {
			changed = true
		}
	} else {
		reasoning := outputDetails.Get("reasoning_tokens")
		if !reasoning.Exists() || reasoning.Type == ojson.Null {
			if errSet := doc.Set(path+".output_tokens_details.reasoning_tokens", 0); errSet == nil {
				changed = true
			}
		}
	}

	inputDetails := usageNode.Get("input_tokens_details")
	if !inputDetails.Exists() {
		if errSet := doc.Set(path+".input_tokens_details.cached_tokens", 0); errSet == nil {
			changed = true
		}
	} else if inputDetails.Type == ojson.Null || !inputDetails.IsObject() {
		if errSet := doc.SetRaw(path+".input_tokens_details", `{"cached_tokens":0}`); errSet == nil {
			changed = true
		}
	} else {
		cached := inputDetails.Get("cached_tokens")
		if !cached.Exists() || cached.Type == ojson.Null {
			if errSet := doc.Set(path+".input_tokens_details.cached_tokens", 0); errSet == nil {
				changed = true
			}
		}
	}

	return changed
}
