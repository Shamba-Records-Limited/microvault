package compliance

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// Named pairs a Screener with the provider name it reports.
type Named struct {
	Name     string
	Screener Screener
}

// verdictRank orders verdicts by strictness: a combined screening takes the
// highest. Pending outranks unscreenable so a provider that has not answered
// never lets an address through on another provider's provisional approval.
var verdictRank = map[Verdict]int{
	VerdictApproved:     0,
	VerdictUnscreenable: 1,
	VerdictPending:      2,
	VerdictReview:       3,
	VerdictRejected:     4,
}

// Stricter returns the stricter of two verdicts; an unknown verdict counts as
// pending.
func Stricter(a, b Verdict) Verdict {
	rank := func(v Verdict) int {
		if r, ok := verdictRank[v]; ok {
			return r
		}
		return verdictRank[VerdictPending]
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// Combine screens with every provider and keeps the strictest verdict. Any
// provider failing fails the whole screening, so an address is never approved
// on a partial answer. A single provider is returned unwrapped.
func Combine(providers ...Named) Screener {
	if len(providers) == 1 {
		return providers[0].Screener
	}
	return combined(providers)
}

type combined []Named

func (c combined) ScreenAddress(ctx context.Context, req ScreenRequest) (*Screening, error) {
	parts := make([]*Screening, len(c))
	g, gctx := errgroup.WithContext(ctx)
	for i, p := range c {
		g.Go(func() error {
			s, err := p.Screener.ScreenAddress(gctx, req)
			if err != nil {
				return err
			}
			if s.Provider == "" {
				s.Provider = p.Name
			}
			parts[i] = s
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	out := &Screening{Address: req.Address, Verdict: VerdictApproved, ScreenedAt: time.Now()}
	names := make([]string, 0, len(parts))
	raw := make(map[string]json.RawMessage, len(parts))
	for _, s := range parts {
		names = append(names, s.Provider)
		out.Verdict = Stricter(out.Verdict, s.Verdict)
		out.Sanctioned = out.Sanctioned || s.Sanctioned
		out.Rules = append(out.Rules, s.Rules...)
		out.Parts = append(out.Parts, *s)
		raw[s.Provider] = s.Raw
	}
	out.Provider = strings.Join(names, "+")
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	out.Raw = b
	return out, nil
}
