package review

import (
	"encoding/json"
	"fmt"
	"strings"
)

// decodeForkJSONReply decodes the last JSON object in a fork's reply that has requiredKey at
// its top level. The key check matters: without a fence, scanning back from the last '{' first
// meets a nested object such as one finding, which decodes cleanly but has no verdict.
func decodeForkJSONReply(reply string, requiredKey string, target any) error {
	if start := strings.LastIndex(reply, "```json"); start >= 0 {
		body := reply[start+len("```json"):]
		if end := strings.Index(body, "```"); end >= 0 {
			body = body[:end]
		}
		if decodeObjectWithKey(strings.NewReader(strings.TrimSpace(body)), requiredKey, target) {
			return nil
		}
	}
	for index := strings.LastIndex(reply, "{"); index >= 0; index = strings.LastIndex(reply[:index], "{") {
		if decodeObjectWithKey(strings.NewReader(reply[index:]), requiredKey, target) {
			return nil
		}
	}
	return fmt.Errorf("fork reply has no JSON object with key %q", requiredKey)
}

func decodeObjectWithKey(reader *strings.Reader, requiredKey string, target any) bool {
	var raw json.RawMessage
	if json.NewDecoder(reader).Decode(&raw) != nil {
		return false
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return false
	}
	if _, ok := keys[requiredKey]; !ok {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}
