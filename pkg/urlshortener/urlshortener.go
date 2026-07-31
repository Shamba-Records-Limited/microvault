package urlshortener

import (
	"context"
	"fmt"

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

// Dub is a [Shortener] backed by dub.co.
type Dub struct {
	links           linkCreator
	domain          string
	imagePreviewURL string
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
		links:           client.Links,
		domain:          opts.Domain,
		imagePreviewURL: opts.ImagePreviewURL,
	}
}

// Shorten creates a dub.co short link for longURL and returns its full short URL.
func (d *Dub) Shorten(ctx context.Context, longURL string) (string, error) {
	body := &operations.CreateLinkRequestBody{URL: longURL}
	if d.domain != "" {
		body.Domain = new(d.domain)
	}
	if d.imagePreviewURL != "" {
		body.Proxy = new(true)
		body.Image = new(d.imagePreviewURL)
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
