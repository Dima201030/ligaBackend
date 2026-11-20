package clubs

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PublicSettingsKeys are the only club settings keys organizers may set via API.
var PublicSettingsKeys = map[string]struct{}{
	"timezone": {},
	"locale":   {},
}

// SanitizeSettings keeps only allow-listed non-secret keys and validates values.
// unknown keys are dropped (never stored).
func SanitizeSettings(raw map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if raw == nil {
		return out, nil
	}
	for key, value := range raw {
		if value == nil {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(key))
		if _, ok := PublicSettingsKeys[k]; !ok {
			continue
		}
		switch k {
		case "timezone":
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("timezone must be a string")
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, err := time.LoadLocation(s); err != nil {
				return nil, fmt.Errorf("unknown timezone %q", s)
			}
			out[k] = s
		case "locale":
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("locale must be a string")
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if len(s) > 32 {
				return nil, fmt.Errorf("locale too long")
			}
			out[k] = s
		}
	}
	return out, nil
}

// MergeSettings overlays patch onto base, both already JSON objects.
// Only PublicSettingsKeys from patch are applied; other base keys are preserved
// except when patch intentionally clears an allow-listed key with null (drop).
func MergeSettings(baseJSON []byte, patch map[string]any) ([]byte, error) {
	base := map[string]any{}
	if len(baseJSON) > 0 {
		if err := json.Unmarshal(baseJSON, &base); err != nil {
			base = map[string]any{}
		}
	}
	clean, err := SanitizeSettings(patch)
	if err != nil {
		return nil, err
	}
	// Explicit nulls in patch remove allow-listed keys.
	for key, value := range patch {
		k := strings.ToLower(strings.TrimSpace(key))
		if _, ok := PublicSettingsKeys[k]; !ok {
			continue
		}
		if value == nil {
			delete(base, k)
			delete(clean, k)
		}
	}
	for k, v := range clean {
		base[k] = v
	}
	// Drop any non-public keys that somehow sit in base before persisting organizer view?
	// Keep legacy unknown keys in DB but organizer API responses will re-sanitize on read.
	return json.Marshal(base)
}

// PublicSettingsJSON returns allow-listed settings only (for organizer responses).
func PublicSettingsJSON(raw []byte) json.RawMessage {
	base := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &base)
	}
	clean, _ := SanitizeSettings(base)
	out, err := json.Marshal(clean)
	if err != nil || len(clean) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(out)
}
