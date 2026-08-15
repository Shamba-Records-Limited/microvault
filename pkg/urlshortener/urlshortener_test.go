package urlshortener

import (
	"context"
	"errors"
	"strings"
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
	// No image configured, but derived title/description still need proxy on.
	if fake.gotBody.Proxy == nil || !*fake.gotBody.Proxy {
		t.Errorf("proxy should be true once a preview field is set, got %v", fake.gotBody.Proxy)
	}
	if fake.gotBody.Image != nil {
		t.Errorf("image should be nil without an image preview URL, got %v", *fake.gotBody.Image)
	}
}

func TestShorten_DerivedPreview(t *testing.T) {
	// The MoneyGram interactive URL: host becomes the title, host+path the
	// description, and the token in the query must not survive into either.
	fake := &fakeLinks{resp: &components.LinkSchema{ShortLink: "https://shmb.us/abc"}}
	d := &Dub{links: fake}

	const mgURL = "https://extstellar.moneygram.com/?transaction_id=abc123&token=SECRET-JWT"
	if _, err := d.Shorten(context.Background(), mgURL); err != nil {
		t.Fatalf("Shorten() error = %v", err)
	}
	if fake.gotBody.Title == nil || *fake.gotBody.Title != "extstellar.moneygram.com" {
		t.Errorf("title = %v, want the destination host", fake.gotBody.Title)
	}
	if fake.gotBody.Description == nil || *fake.gotBody.Description != "Visit extstellar.moneygram.com/" {
		t.Errorf("description = %v, want %q", fake.gotBody.Description, "Visit extstellar.moneygram.com/")
	}
	for _, field := range []*string{fake.gotBody.Title, fake.gotBody.Description} {
		if field != nil && strings.Contains(*field, "SECRET-JWT") {
			t.Errorf("query token leaked into a preview field: %q", *field)
		}
	}
}

func TestShorten_PreviewOverrides(t *testing.T) {
	fake := &fakeLinks{resp: &components.LinkSchema{ShortLink: "https://shmb.us/abc"}}
	d := &Dub{links: fake, previewTitle: "MicroVault", previewDescription: "Collect your cash"}

	if _, err := d.Shorten(context.Background(), "https://extstellar.moneygram.com/?t=x"); err != nil {
		t.Fatalf("Shorten() error = %v", err)
	}
	if fake.gotBody.Title == nil || *fake.gotBody.Title != "MicroVault" {
		t.Errorf("title = %v, want the override", fake.gotBody.Title)
	}
	if fake.gotBody.Description == nil || *fake.gotBody.Description != "Collect your cash" {
		t.Errorf("description = %v, want the override", fake.gotBody.Description)
	}
}

func TestPreview_PartialOverrideAndPaths(t *testing.T) {
	for name, tc := range map[string]struct {
		dub             Dub
		longURL         string
		wantTitle       string
		wantDescription string
	}{
		"title override, description derived": {
			dub: Dub{previewTitle: "MicroVault"}, longURL: "https://mg.example/support/x",
			wantTitle: "MicroVault", wantDescription: "Visit mg.example/support/x",
		},
		"description override, title derived": {
			dub: Dub{previewDescription: "Collect your cash"}, longURL: "https://mg.example/support/x",
			wantTitle: "mg.example", wantDescription: "Collect your cash",
		},
		"path preserved": {
			dub: Dub{}, longURL: "https://mg.example/a/b?q=1#frag",
			wantTitle: "mg.example", wantDescription: "Visit mg.example/a/b",
		},
		"unparseable URL yields no preview": {
			dub: Dub{}, longURL: "::not a url::",
			wantTitle: "", wantDescription: "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			title, description := tc.dub.preview(tc.longURL)
			if title != tc.wantTitle {
				t.Errorf("title = %q, want %q", title, tc.wantTitle)
			}
			if description != tc.wantDescription {
				t.Errorf("description = %q, want %q", description, tc.wantDescription)
			}
		})
	}
}

func TestTruncate_RespectsDubLimits(t *testing.T) {
	// dub caps title at 120 and description at 240; over-long values must be
	// cut by rune so multi-byte characters are not split.
	d := NewDub(DubOptions{
		APIKey:             "key",
		PreviewTitle:       strings.Repeat("é", 200),
		PreviewDescription: strings.Repeat("é", 300),
	})
	if got := len([]rune(d.previewTitle)); got != maxPreviewTitleLen {
		t.Errorf("title runes = %d, want %d", got, maxPreviewTitleLen)
	}
	if got := len([]rune(d.previewDescription)); got != maxPreviewDescriptionLen {
		t.Errorf("description runes = %d, want %d", got, maxPreviewDescriptionLen)
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

func TestShorten_Domain(t *testing.T) {
	fake := &fakeLinks{resp: &components.LinkSchema{ShortLink: "https://shmb.us/abc"}}
	d := &Dub{links: fake, domain: "shmb.us"}

	if _, err := d.Shorten(context.Background(), "https://example.com/long"); err != nil {
		t.Fatalf("Shorten() error = %v", err)
	}
	if fake.gotBody.Domain == nil || *fake.gotBody.Domain != "shmb.us" {
		t.Errorf("domain = %v, want the configured domain", fake.gotBody.Domain)
	}
}

func TestShorten_NoDomain(t *testing.T) {
	// An unset domain must be omitted so the workspace default applies, not
	// sent as an empty string dub would reject.
	fake := &fakeLinks{resp: &components.LinkSchema{ShortLink: "https://shmb.us/abc"}}
	d := &Dub{links: fake}

	if _, err := d.Shorten(context.Background(), "https://example.com/long"); err != nil {
		t.Fatalf("Shorten() error = %v", err)
	}
	if fake.gotBody.Domain != nil {
		t.Errorf("domain = %v, want nil when unconfigured", *fake.gotBody.Domain)
	}
}

func TestNewDub_BaseURL(t *testing.T) {
	// NewDub must not panic on either a self-hosted or a default base URL; the
	// SDK server URL itself is not observable through the linkCreator seam.
	for name, baseURL := range map[string]string{
		"self-hosted": "https://api.shmb.us",
		"default":     "",
	} {
		t.Run(name, func(t *testing.T) {
			d := NewDub(DubOptions{APIKey: "key", BaseURL: baseURL, Domain: "shmb.us"})
			if d.links == nil {
				t.Error("NewDub() left links nil")
			}
			if d.domain != "shmb.us" {
				t.Errorf("domain = %q, want %q", d.domain, "shmb.us")
			}
		})
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
