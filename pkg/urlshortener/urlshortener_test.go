package urlshortener

import (
	"context"
	"errors"
	"testing"

	"github.com/dubinc/dub-go/models/components"
	"github.com/dubinc/dub-go/models/operations"
)

// fakeLinks stands in for *dubgo.Links, capturing the request and returning a
// canned response so Shorten can be tested without a live dub.co call.
type fakeLinks struct {
	resp    *components.LinkSchema
	err     error
	gotBody *operations.CreateLinkRequestBody
}

func (f *fakeLinks) Create(_ context.Context, req *operations.CreateLinkRequestBody, _ ...operations.Option) (*components.LinkSchema, error) {
	f.gotBody = req
	return f.resp, f.err
}

func TestShorten_Success(t *testing.T) {
	fake := &fakeLinks{resp: &components.LinkSchema{ShortLink: "https://mgv.link/abc"}}
	d := &Dub{links: fake}

	got, err := d.Shorten(context.Background(), "https://example.com/very/long/url")
	if err != nil {
		t.Fatalf("Shorten() error = %v", err)
	}
	if got != "https://mgv.link/abc" {
		t.Errorf("Shorten() = %q, want %q", got, "https://mgv.link/abc")
	}
	if fake.gotBody.URL != "https://example.com/very/long/url" {
		t.Errorf("request URL = %q, want the long URL", fake.gotBody.URL)
	}
	// No image preview configured: proxy/image must be left unset.
	if fake.gotBody.Proxy != nil || fake.gotBody.Image != nil {
		t.Errorf("proxy/image should be nil without an image preview URL, got proxy=%v image=%v",
			fake.gotBody.Proxy, fake.gotBody.Image)
	}
}

func TestShorten_WithImagePreview(t *testing.T) {
	fake := &fakeLinks{resp: &components.LinkSchema{ShortLink: "https://mgv.link/xyz"}}
	d := &Dub{links: fake, imagePreviewURL: "https://cdn.example.com/og.png"}

	if _, err := d.Shorten(context.Background(), "https://example.com/long"); err != nil {
		t.Fatalf("Shorten() error = %v", err)
	}
	if fake.gotBody.Proxy == nil || !*fake.gotBody.Proxy {
		t.Errorf("proxy should be true when an image preview URL is set, got %v", fake.gotBody.Proxy)
	}
	if fake.gotBody.Image == nil || *fake.gotBody.Image != "https://cdn.example.com/og.png" {
		t.Errorf("image = %v, want the configured preview URL", fake.gotBody.Image)
	}
}

func TestShorten_ClientError(t *testing.T) {
	fake := &fakeLinks{err: errors.New("boom")}
	d := &Dub{links: fake}

	got, err := d.Shorten(context.Background(), "https://example.com/long")
	if err == nil {
		t.Fatal("Shorten() expected an error, got nil")
	}
	if got != "" {
		t.Errorf("Shorten() = %q, want empty on error", got)
	}
}

func TestShorten_EmptyShortLink(t *testing.T) {
	// A nil link and a present-but-empty ShortLink must both be errors, not a
	// silently broken link handed to an SMS.
	for name, resp := range map[string]*components.LinkSchema{
		"nil link":         nil,
		"empty short link": {ShortLink: ""},
	} {
		t.Run(name, func(t *testing.T) {
			d := &Dub{links: &fakeLinks{resp: resp}}
			if _, err := d.Shorten(context.Background(), "https://example.com/long"); err == nil {
				t.Error("Shorten() expected an error for an unusable link, got nil")
			}
		})
	}
}
