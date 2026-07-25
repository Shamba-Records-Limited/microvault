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
	imagePreviewURL string
}

// NewDub builds a dub.co-backed shortener. imagePreviewURL is optional; when
// set, links use dub Custom Link Previews (og:image) for a rich SMS preview.
func NewDub(apiKey, imagePreviewURL string) *Dub {
	client := dubgo.New(dubgo.WithSecurity(apiKey))
	return &Dub{
		links:           client.Links,
		imagePreviewURL: imagePreviewURL,
	}
}

// Shorten creates a dub.co short link for longURL and returns its full short URL.
func (d *Dub) Shorten(ctx context.Context, longURL string) (string, error) {
	body := &operations.CreateLinkRequestBody{URL: longURL}
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
