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
// dub ignores og:title, og:description and og:image unless proxy is set, and
// falls back to its own branding when they are absent, so Shorten always sends
// a title and description. DubOptions.PreviewTitle and PreviewDescription
// override them; left empty they are derived from the destination — host as the
// title, "Visit host/path" as the description. Derivation drops the query and
// fragment: the MoneyGram interactive URL carries a session token there, and an
// og:description is scraped and cached by carriers and link-preview bots.
//
// Shorten treats a nil or empty short link from dub as an error rather than
// returning a broken link, so callers can fall back to the unshortened URL
// rather than send something that does not resolve.
package urlshortener
