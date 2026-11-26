package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

type errorBody struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Code: code, Message: message})
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// prefersHTML reports whether the client likely wants an HTML page (browser link click).
func prefersHTML(r *http.Request) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	if accept == "" {
		return true
	}
	if strings.Contains(accept, "text/html") {
		return true
	}
	if strings.Contains(accept, "application/json") {
		return false
	}
	return !strings.Contains(accept, "application/")
}
