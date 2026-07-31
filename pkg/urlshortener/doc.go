// Package urlshortener turns long outbound links — MoneyGram cash-pickup
// interactive and support URLs — into short, tappable links that fit inside an
// SMS segment.
//
// Shortener is the one-method port the rest of the platform depends on; Dub is
// the only implementation, backed by dub. DubOptions.BaseURL points it at a
// self-hosted instance; left empty it uses the SDK default, https://api.dub.co.
// Self-hosted instances have no dub.sh, so DubOptions.Domain normally names the
// short-link domain registered in the workspace. When an image preview URL is
// configured, links are created with dub Custom Link Previews (proxy plus
// og:image) so the SMS renders a rich preview.
//
// Shorten treats a nil or empty short link from dub as an error rather than
// returning a broken link, so callers can fall back to the unshortened URL
// rather than send something that does not resolve.
package urlshortener
