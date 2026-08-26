package urlshortener

import (
	"context"
	"fmt"
	"net/url"

	dubgo "github.com/dubinc/dub-go"
	"github.com/dubinc/dub-go/models/components"
	"github.com/dubinc/dub-go/models/operations"
)

// Shortener turns a long URL into a short one.
type Shortener interface {
	Shorten(ctx context.Context, longURL string) (string, error)
}

// linkCreator is the slice of the dub.co SDK that Dub depends on. Narrowing it
// to the one method used keeps Shorten unit-testable with a fake, without a
// live dub.co call. *dubgo.Links satisfies it.
type linkCreator interface {
	Create(ctx context.Context, request *operations.CreateLinkRequestBody, opts ...operations.Option) (*components.LinkSchema, error)
}

var _ linkCreator = (*dubgo.Links)(nil)

// dub caps Custom Link Preview text at these lengths.
const (
	maxPreviewTitleLen       = 120
	maxPreviewDescriptionLen = 240
)

// Dub is a [Shortener] backed by dub.co.
type Dub struct {
	links              linkCreator
	domain             string
	previewTitle       string
	previewDescription string
	imagePreviewURL    string
}

// DubOptions configures a [Dub] shortener. Only APIKey is required.
type DubOptions struct {
	// APIKey is the dub workspace API key.
	APIKey string
	// BaseURL points the SDK at a self-hosted dub instance. Empty uses the
	// SDK default, https://api.dub.co.
	BaseURL string
	// Domain is the short-link domain to create links under. Empty lets the
	// workspace default apply — self-hosted instances have no dub.sh, so this
	// is normally set.
	Domain string
	// PreviewTitle overrides the og:title. Empty derives it from the
	// destination host. Truncated to 120 characters.
	PreviewTitle string
	// PreviewDescription overrides the og:description. Empty derives it from
	// the destination host and path. Truncated to 240 characters.
	PreviewDescription string
	// ImagePreviewURL is the og:image for dub Custom Link Previews, giving a
	// rich SMS preview. Optional.
	ImagePreviewURL string
}

// NewDub builds a dub-backed shortener.
func NewDub(opts DubOptions) *Dub {
	sdkOpts := []dubgo.SDKOption{dubgo.WithSecurity(opts.APIKey)}
	if opts.BaseURL != "" {
		sdkOpts = append(sdkOpts, dubgo.WithServerURL(opts.BaseURL))
	}
	client := dubgo.New(sdkOpts...)
	return &Dub{
		links:              client.Links,
		domain:             opts.Domain,
		previewTitle:       truncate(opts.PreviewTitle, maxPreviewTitleLen),
		previewDescription: truncate(opts.PreviewDescription, maxPreviewDescriptionLen),
		imagePreviewURL:    opts.ImagePreviewURL,
	}
}

// preview returns the og:title and og:description for longURL: the configured
// overrides when set, else derived from the destination.
func (d *Dub) preview(longURL string) (title, description string) {
	title, description = d.previewTitle, d.previewDescription
	if title != "" && description != "" {
		return title, description
	}

	u, err := url.Parse(longURL)
	if err != nil || u.Host == "" {
		return title, description
	}
	if title == "" {
		title = truncate(u.Host, maxPreviewTitleLen)
	}
	if description == "" {
		path := u.EscapedPath()
		if path == "" {
			path = "/"
		}
		description = truncate("Visit "+u.Host+path, maxPreviewDescriptionLen)
	}
	return title, description
}

// truncate caps s at max runes, so a multi-byte title is not cut mid-character.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// Shorten creates a dub.co short link for longURL and returns its full short URL.
func (d *Dub) Shorten(ctx context.Context, longURL string) (string, error) {
	body := &operations.CreateLinkRequestBody{URL: longURL}
	if d.domain != "" {
		body.Domain = new(d.domain)
	}

	// dub ignores title/description/image unless proxy is on, and falls back to
	// its own branding when they are absent.
	title, description := d.preview(longURL)
	if title != "" {
		body.Title = new(title)
	}
	if description != "" {
		body.Description = new(description)
	}
	if d.imagePreviewURL != "" {
		body.Image = new(d.imagePreviewURL)
	}
	if body.Title != nil || body.Description != nil || body.Image != nil {
		body.Proxy = new(true)
	}

	link, err := d.links.Create(ctx, body)
	if err != nil {
		return "", fmt.Errorf("dub create link: %w", err)
	}
	if link == nil || link.ShortLink == "" {
		return "", fmt.Errorf("dub create link: empty short link")
	}
	return link.ShortLink, nil
}
