package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	httpapi "github.com/Dima201030/ligaBackend/internal/http"
)

func TestLiveMatchRoutesRegistered(t *testing.T) {
	t.Parallel()
	srv := httpapi.New(nil)

	cases := []struct {
		method string
		path   string
		// Without a store these handlers fail after routing; we only assert they are wired
		// (not 404 from the router itself).
		wantNot404 bool
	}{
		{http.MethodGet, "/api/tournaments/demo/matches/sync", true},
		{http.MethodGet, "/api/tournaments/demo/matches/events", true},
		{http.MethodGet, "/api/organizer/tournaments/00000000-0000-0000-0000-000000000001/matches/sync", true},
		{http.MethodGet, "/api/organizer/tournaments/00000000-0000-0000-0000-000000000001/matches/events", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if tc.wantNot404 && rec.Code == http.StatusNotFound {
				t.Fatalf("route missing: status=404 body=%s", rec.Body.String())
			}
		})
	}
}
