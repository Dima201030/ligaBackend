package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type checkResult struct {
	Status    string `json:"status"`
	LatencyMS *int64 `json:"latency_ms,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type healthResponse struct {
	Status  string                 `json:"status"`
	Service string                 `json:"service"`
	Time    string                 `json:"time"`
	Checks  map[string]checkResult `json:"checks"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	checks := map[string]checkResult{
		"api": {
			Status: "ok",
			Detail: "process up",
		},
	}

	dbStatus := "unknown"
	dbDetail := "store not configured"
	var dbLatency *int64

	if s.store != nil && s.store.Pool != nil {
		start := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		err := s.store.Pool.Ping(ctx)
		cancel()
		ms := time.Since(start).Milliseconds()
		dbLatency = &ms
		if err != nil {
			dbStatus = "down"
			dbDetail = "ping failed"
		} else {
			dbStatus = "ok"
			dbDetail = "ping ok"
		}
	}

	checks["database"] = checkResult{
		Status:    dbStatus,
		LatencyMS: dbLatency,
		Detail:    dbDetail,
	}

	overall := "ok"
	for _, c := range checks {
		if c.Status == "down" || c.Status == "error" {
			overall = "down"
			break
		}
		if c.Status == "degraded" || c.Status == "unknown" {
			overall = "degraded"
		}
	}

	code := http.StatusOK
	if overall == "down" {
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  overall,
		Service: "liga-api",
		Time:    time.Now().UTC().Format(time.RFC3339),
		Checks:  checks,
	})
}
