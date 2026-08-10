package stellaranchor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type stubRoundTripper struct {
	calls int
	errs  []error // errs[i] returned on call i; nil ⇒ 200 OK
}

func (s *stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	i := s.calls
	s.calls++
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
	}, nil
}

func TestIsTransientNetErr(t *testing.T) {
	transient := []error{
		errors.New("http2: server sent GOAWAY and closed the connection; LastStreamID=3, ErrCode=NO_ERROR"),
		errors.New("read: connection reset by peer"),
		io.ErrUnexpectedEOF,
	}
	for _, err := range transient {
		if !isTransientNetErr(err) {
			t.Errorf("expected transient: %v", err)
		}
	}

	nonTransient := []error{nil, errors.New("400 Bad Request"), ErrUnauthorized}
	for _, err := range nonTransient {
		if isTransientNetErr(err) {
			t.Errorf("expected non-transient: %v", err)
		}
	}
}

func TestDoHTTPWithRetry_RecoversFromGOAWAY(t *testing.T) {
	rt := &stubRoundTripper{errs: []error{
		errors.New("http2: server sent GOAWAY and closed the connection"),
	}}
	client := &http.Client{Transport: rt}

	resp, err := doHTTPWithRetry(context.Background(), client, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "http://anchor.test/auth", nil)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if rt.calls != 2 {
		t.Errorf("expected 2 attempts (fail then succeed), got %d", rt.calls)
	}
}

func TestDoHTTPWithRetry_NonTransientFailsFast(t *testing.T) {
	rt := &stubRoundTripper{errs: []error{errors.New("no such host")}}
	client := &http.Client{Transport: rt}

	_, err := doHTTPWithRetry(context.Background(), client, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "http://anchor.test/auth", nil)
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if rt.calls != 1 {
		t.Errorf("expected 1 attempt (no retry), got %d", rt.calls)
	}
}
