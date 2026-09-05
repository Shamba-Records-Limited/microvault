package mpesa

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnvironment(t *testing.T) {
	if EnvironmentSandbox.BaseURL() != sandboxBaseURL {
		t.Errorf("sandbox base URL = %q", EnvironmentSandbox.BaseURL())
	}
	if EnvironmentProduction.BaseURL() != productionBaseURL {
		t.Errorf("production base URL = %q", EnvironmentProduction.BaseURL())
	}
	if !EnvironmentProduction.IsProduction() || EnvironmentSandbox.IsProduction() {
		t.Error("IsProduction is wrong")
	}
	if !EnvironmentSandbox.Valid() || !EnvironmentProduction.Valid() || Environment("staging").Valid() {
		t.Error("Valid is wrong")
	}
	// An unknown environment must not silently resolve to production.
	if Environment("").BaseURL() != sandboxBaseURL {
		t.Error("an unknown environment resolved to something other than sandbox")
	}
}

func TestNew_Validation(t *testing.T) {
	cases := map[string]Config{
		"unknown environment": {Environment: "staging", ConsumerKey: "k", ConsumerSecret: "s"},
		"empty environment":   {ConsumerKey: "k", ConsumerSecret: "s"},
		"no consumer key":     {Environment: EnvironmentSandbox, ConsumerSecret: "s"},
		"no consumer secret":  {Environment: EnvironmentSandbox, ConsumerKey: "k"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(cfg); err == nil {
				t.Error("expected a validation error")
			}
		})
	}
}

func TestNew_Defaults(t *testing.T) {
	c, err := New(Config{Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.http == nil || c.tokens == nil || c.now == nil || c.mint == nil {
		t.Error("a default collaborator was left nil")
	}
	if len(c.certificate) == 0 {
		t.Error("no certificate was loaded")
	}
	if c.baseURL != sandboxBaseURL {
		t.Errorf("baseURL = %q", c.baseURL)
	}
	if c.Environment() != EnvironmentSandbox {
		t.Errorf("Environment() = %q", c.Environment())
	}
}

func TestNew_TrimsTrailingSlashFromBaseURL(t *testing.T) {
	c, err := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		BaseURL: "https://stub.test/",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.baseURL != "https://stub.test" {
		t.Errorf("baseURL = %q, want no trailing slash", c.baseURL)
	}
}

func TestNew_ShortcodesAreIndependent(t *testing.T) {
	c, _ := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		CollectionShortcode: 174379, DisbursementShortcode: 600997,
	})
	if c.CollectionShortcode() != 174379 || c.DisbursementShortcode() != 600997 {
		t.Errorf("shortcodes = %d, %d", c.CollectionShortcode(), c.DisbursementShortcode())
	}
}

type failingTransport struct{}

func (failingTransport) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp: connection refused")
}

func TestSend_TransportFailure(t *testing.T) {
	c, _ := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		HttpClient: failingTransport{},
	})
	type payload struct{}
	if _, err := send[payload](context.Background(), c, mpesaErr("test"), http.MethodGet, "/x", nil, nil); err == nil {
		t.Error("expected a transport error")
	}
}

func TestSend_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	c, _ := New(Config{Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s", BaseURL: srv.URL})
	type payload struct{}
	if _, err := send[payload](context.Background(), c, mpesaErr("test"), http.MethodGet, "/x", nil, nil); err == nil {
		t.Error("expected a decode error")
	}
}

func TestSend_SetsJSONHeaders(t *testing.T) {
	var contentType, accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType, accept = r.Header.Get("Content-Type"), r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s", BaseURL: srv.URL})
	type payload struct{}
	if _, err := send[payload](context.Background(), c, mpesaErr("test"), http.MethodPost, "/x", map[string]int{"a": 1}, nil); err != nil {
		t.Fatalf("send: %v", err)
	}
	if contentType != "application/json" || accept != "application/json" {
		t.Errorf("headers = %q, %q", contentType, accept)
	}
}

func TestSend_UnencodableBody(t *testing.T) {
	c, _ := New(Config{Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s"})
	type payload struct{}
	if _, err := send[payload](context.Background(), c, mpesaErr("test"), http.MethodPost, "/x", make(chan int), nil); err == nil {
		t.Error("expected an encode error")
	}
}

func TestSend_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s", BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	type payload struct{}
	if _, err := send[payload](ctx, c, mpesaErr("test"), http.MethodGet, "/x", nil, nil); err == nil {
		t.Error("expected a cancelled context to fail the request")
	}
}
