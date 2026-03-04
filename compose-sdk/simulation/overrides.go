package simulation

import (
	"bytes"
	"encoding/json"
)

// CloneStateOverrides creates a deep copy of a state overrides map via JSON
// round-trip. Returns nil for nil input.
func CloneStateOverrides(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	data, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	var dst map[string]any
	if err := json.Unmarshal(data, &dst); err != nil {
		return nil
	}
	return dst
}

// ParseStateOverrides decodes a JSON-encoded state overrides blob.
// Returns nil for empty, null, or invalid input.
func ParseStateOverrides(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
