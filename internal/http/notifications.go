package httpapi

import (
	"encoding/json"
	"fmt"
)

// notificationPrefsJSON is the server-side notification preference set.
type notificationPrefsJSON struct {
	Tournaments bool `json:"tournaments"`
	Reminders   bool `json:"reminders"`
	Results     bool `json:"results"`
	News        bool `json:"news"`
}

func defaultNotificationPrefs() notificationPrefsJSON {
	return notificationPrefsJSON{
		Tournaments: true,
		Reminders:   true,
		Results:     true,
		News:        false,
	}
}

func encodeNotificationPrefs(p notificationPrefsJSON) ([]byte, error) {
	return json.Marshal(p)
}

func decodeNotificationPrefs(raw []byte) notificationPrefsJSON {
	out := defaultNotificationPrefs()
	if len(raw) == 0 {
		return out
	}
	var parsed map[string]bool
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return out
	}
	if v, ok := parsed["tournaments"]; ok {
		out.Tournaments = v
	}
	if v, ok := parsed["reminders"]; ok {
		out.Reminders = v
	}
	if v, ok := parsed["results"]; ok {
		out.Results = v
	}
	if v, ok := parsed["news"]; ok {
		out.News = v
	}
	return out
}

func mergeNotificationPrefs(current notificationPrefsJSON, patch map[string]*bool) (notificationPrefsJSON, error) {
	out := current
	for key, value := range patch {
		if value == nil {
			continue
		}
		switch key {
		case "tournaments":
			out.Tournaments = *value
		case "reminders":
			out.Reminders = *value
		case "results":
			out.Results = *value
		case "news":
			out.News = *value
		default:
			return current, fmt.Errorf("unknown preference %q", key)
		}
	}
	return out, nil
}
