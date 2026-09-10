package mpesa

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// callbackTarget stands in for our own callback controller, which does not
// exist yet: this package is a client library and the controller is wiring.
func callbackTarget(t *testing.T, received chan<- []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/callbacks/daraja/abc/result"
}
