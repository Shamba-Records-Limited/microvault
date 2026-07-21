// Package urlshortener turns long outbound links (e.g. MoneyGram cash-pickup
// interactive URLs) into short, tappable SMS links via dub.co.
package urlshortener

import (
	"context"
	"fmt"

	dubgo "github.com/dubinc/dub-go"
	"github.com/dubinc/dub-go/models/operations"
)

// Shortener turns a long URL into a short one.
type Shortener interface {
	Shorten(ctx context.Context, longURL string) (string, error)
}

// Dub is a [Shortener] backed by dub.co.
type Dub struct {
	client          *dubgo.Dub
	imagePreviewURL string
}

// NewDub builds a dub.co-backed shortener. imagePreviewURL is optional; when
// set, links use dub Custom Link Previews (og:image) for a rich SMS preview.
func NewDub(apiKey, imagePreviewURL string) *Dub {
	return &Dub{
		client:          dubgo.New(dubgo.WithSecurity(apiKey)),
		imagePreviewURL: imagePreviewURL,
	}
}

// Shorten creates a dub.co short link for longURL and returns its full short URL.
func (d *Dub) Shorten(ctx context.Context, longURL string) (string, error) {
	body := &operations.CreateLinkRequestBody{URL: longURL}
	if d.imagePreviewURL != "" {
		body.Proxy = dubgo.Pointer(true)
		body.Image = dubgo.Pointer(d.imagePreviewURL)
	}

	link, err := d.client.Links.Create(ctx, body)
	if err != nil {
		return "", fmt.Errorf("dub create link: %w", err)
	}
	if link == nil || link.ShortLink == "" {
		return "", fmt.Errorf("dub create link: empty short link")
	}
	return link.ShortLink, nil
}
