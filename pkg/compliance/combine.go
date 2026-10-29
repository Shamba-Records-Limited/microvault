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
	names := make([]string, len(c))
	for i, p := range c {
		names[i] = p.Name
	}
	return merge(ctx, names, req.Address, "", func(ctx context.Context, i int) (*Screening, error) {
		return c[i].Screener.ScreenAddress(ctx, req)
	})
}

// NamedTx pairs a TransactionScreener with the provider name it reports.
type NamedTx struct {
	Name     string
	Screener TransactionScreener
}

// CombineTx screens a transaction with every provider and keeps the
// strictest verdict, as Combine does for addresses.
func CombineTx(providers ...NamedTx) TransactionScreener {
	if len(providers) == 1 {
		return providers[0].Screener
	}
	return combinedTx(providers)
}

type combinedTx []NamedTx

func (c combinedTx) ScreenTransaction(ctx context.Context, req TxScreenRequest) (*Screening, error) {
	names := make([]string, len(c))
	for i, p := range c {
		names[i] = p.Name
	}
	return merge(ctx, names, req.Address, req.Hash, func(ctx context.Context, i int) (*Screening, error) {
		return c[i].Screener.ScreenTransaction(ctx, req)
	})
}

// merge runs one screening per provider in parallel and folds the results.
func merge(ctx context.Context, providers []string, address, hash string, screen func(context.Context, int) (*Screening, error)) (*Screening, error) {
	parts := make([]*Screening, len(providers))
	g, gctx := errgroup.WithContext(ctx)
	for i := range providers {
		g.Go(func() error {
			s, err := screen(gctx, i)
			if err != nil {
				return err
			}
			if s.Provider == "" {
				s.Provider = providers[i]
			}
			parts[i] = s
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	out := &Screening{Address: address, TxHash: hash, Verdict: VerdictApproved, ScreenedAt: time.Now()}
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
