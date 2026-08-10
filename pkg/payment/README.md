# Payments Architecture

How the payment surface is structured, why it's structured that way, and what
you need to know to add a new off-ramp provider or consume the contract from
any channel i.e. USSD, WhatsApp, Telegram or web.

This document describes the deliberate shape of `pkg/payment/`. If you're
making changes here, follow the principles below; if you're adding a feature
that *needs* to break them, write down why in the PR.

## Contents

- [Design principles](#design-principles)
- [Package layout](#package-layout)
- [Adding a new off-ramp provider](#adding-a-new-off-ramp-provider)
- [Adding a new Stellar anchor](#adding-a-new-stellar-anchor)
- [Consuming the contract](#consuming-the-contract)
- [What lives where, and why](#what-lives-where-and-why)

---

## Design principles

Four principles shape the package layout.

### 1. Channel-agnostic contract

`pkg/payment/offramp` owns the off-ramp interfaces. It does **not** know about
USSD, bots, HTTP handlers, or any specific delivery channel. The same
`offramp.Provider` is consumed by `pkg/mobile/ussd/adapters` today and will be
consumed by future chat-bot adapters without modification.

If you find yourself wanting to import a channel package from `pkg/payment/`,
that's a layering inversion; push the channel concern back into the channel
package.

### 2. Capability composition over fat interface

Each distinct capability is its own narrow interface:

```go
// pkg/payment/offramp/offramp.go

type Provider interface {
    ID() ProviderID
    Initiate(ctx context.Context, req Request) (*Result, error)
}

type StatusReader interface {
    Status(ctx context.Context, ref ProviderRef) (*Status, error)
}

type Quoter interface {
    Quote(ctx context.Context, q QuoteRequest) (*ExchangeRate, error)
}

type Directory interface {
    SupportedProviders(ctx context.Context, country string) ([]ProviderInfo, error)
}

type MobileMoneyDirectory interface {
    Networks(ctx context.Context, country string) ([]MobileMoneyNetwork, error)
}

type BalanceReporter interface {
    AvailableBalance(ctx context.Context) (float64, error)
}
```

Providers implement only the capabilities they actually support. Callers
type-assert for what they need:

```go
if d, ok := provider.(offramp.MobileMoneyDirectory); ok {
    networks, _ := d.Networks(ctx, "KE")
}
```

This is the standard library's pattern (`io.Reader`, `io.Closer`, `io.Seeker`
— small interfaces composed where needed). Two consequences:

- **Honest implementations.** A provider implements a capability only when it
  genuinely supports it. MoneyGram has no MoMo concept, so it does not
  implement `MobileMoneyDirectory`; `provider.(MobileMoneyDirectory)` fails
  for MG and callers handle that explicitly.
- **Cheap to extend.** A new capability (e.g. `Refunder` for providers that
  expose a refund API) is a new interface in this file. Existing providers
  don't change.

The mandatory minimum is `Provider`. Every off-ramp must satisfy it.

### 3. Core + typed payload

`Request` and `Result` carry only **cross-provider** fields directly. Anything
provider-specific lives in a typed payload attached to the struct:

```go
type Request struct {
    LoanID, UserID, RecipientName, DestinationPhone, CountryCode string
    AmountUSD     float64
    AmountStroops int64
    // ... cross-cutting fields
    PayoutMethod  string
    Options       ProviderOptions   // typed extras, e.g. yellowcard.Options
}

type Result struct {
    RequestID, Status, LocalCurrency string
    AmountUSD, AmountLocal float64
    // ... cross-cutting summary fields
    Provider ProviderPayload         // typed extras, e.g. moneygram.CashPickupPayload
}

type ProviderOptions interface { ProviderID() ProviderID }
type ProviderPayload interface { ProviderID() ProviderID }
```

Concrete payload types live in their **provider's** package, not in offramp:

```go
// pkg/payment/yellowcard/payload.go
type Options struct {
    SettlementMethod string // "direct" | "fiat"
}
func (Options) ProviderID() offramp.ProviderID { return offramp.ProviderYellowCard }

type DirectSettlementPayload struct {
    StellarAddress, StellarMemo, StellarTxHash string
}
func (DirectSettlementPayload) ProviderID() offramp.ProviderID { return offramp.ProviderYellowCard }
```

```go
// pkg/payment/moneygram/payload.go
type Options struct {
    BirthDate         string
    ChildAccountIndex uint32
}
func (Options) ProviderID() offramp.ProviderID { return offramp.ProviderMoneyGram }

type CashPickupPayload struct {
    InteractiveURL, ExternalReference, MoreInfoURL string
    ChildAccountMemo                                int64
    WithdrawMemo, WithdrawMemoType                  string
}
func (CashPickupPayload) ProviderID() offramp.ProviderID { return offramp.ProviderMoneyGram }
```

Adding a third provider means a third payload type in that provider's
package; the `Request` and `Result` shapes in `offramp` stay untouched.

Consumers that care about the typed payload assert:

```go
res, _ := provider.Initiate(ctx, req)
if pl, ok := res.Provider.(moneygram.CashPickupPayload); ok {
    persistInteractiveURL(pl.InteractiveURL)
}
```

Consumers that only render `res.AmountLocal` and `res.Status` ignore the
payload entirely.

### 4. Registry-based routing

A registry resolves the right provider for each incoming request:

```go
// pkg/payment/offramp/registry.go

type Registry struct { /* providers, aliases */ }

func (r *Registry) Register(p Provider) error
func (r *Registry) Alias(payoutMethod string, id ProviderID) error
func (r *Registry) Resolve(req Request) (Provider, error)
func (r *Registry) Get(id ProviderID) (Provider, bool)
func (r *Registry) All() []Provider
```

Resolution order:

1. If `req.Options` is set, use `req.Options.ProviderID()`. The caller has
   pinned a specific provider by attaching its typed options.
2. Otherwise, look up `req.PayoutMethod` in the alias table. Empty
   `PayoutMethod` defaults to `PayoutMethodMobileMoney`.

A new provider is registered at boot:

```go
reg := offramp.NewRegistry()
_ = reg.Register(ycAdapter) // ID() returns offramp.ProviderYellowCard
_ = reg.Register(mgAdapter) // ID() returns offramp.ProviderMoneyGram
_ = reg.Alias(offramp.PayoutMethodMobileMoney, offramp.ProviderYellowCard)
_ = reg.Alias(offramp.PayoutMethodCashPickup,  offramp.ProviderMoneyGram)
```

No glue file to edit, no enum to extend.

---

## Package layout

```
pkg/payment/
├── offramp/                # the channel-agnostic contract
│   ├── offramp.go          # Provider + capability interfaces + Request/Result
│   ├── registry.go         # Registry: Register, Alias, Resolve, Get, All
│   └── noop.go             # NoOpProvider for tests
│
├── stellaranchor/          # reusable Stellar-anchor primitives
│   ├── client.go           # composes Auth + Anchor + JWTCache
│   ├── sep1.go             # TOML fetch + Validate
│   ├── sep9.go             # SEP-9 Customer + SplitFullName
│   ├── sep10.go            # challenge/cosign/JWT submit
│   ├── sep24.go            # withdraw/initiate + transaction + Status enum
│   ├── jwt_cache.go        # per-memo JWT cache
│   ├── memo.go             # ChildAccountMemo derivation
│   ├── iso.go              # ISO-2 to ISO-3 mapping
│   └── errors.go           # Protocol and custom errors
│
├── moneygram/              # MG-flavoured anchor: stellaranchor + REST FX
│   ├── client.go           # embeds *stellaranchor.Client; adds OAuth + FXRate
│   ├── payload.go          # Options, CashPickupPayload
│   ├── oauth.go            # MG REST OAuth client_credentials
│   ├── fxrate.go           # GET /fx-rate/v1/rates (corridor cache)
│   ├── fx.go               # FXOrchestrator (cascading rate source)
│   └── errors.go           # Moneygram-specific errors
│
├── yellowcard/             # YC mobile-money adapter
│   ├── yellowcard.go       # HTTP client + HMAC signing
│   ├── payload.go          # Options, DirectSettlementPayload, FiatPayload
│   ├── types.go            # YC API request/response shapes
│   └── errors.go           # YellowCard-specific errors
│
└── fonbnk/                 # placeholder — auth transport only
    └── fonbnk.go
```

Glue (the *adapter* that translates between USSD-shaped data and an
`offramp.Provider`) lives in `pkg/mobile/ussd/adapters/`. Bots will get their
own adapter packages alongside it.

---

## Adding a new off-ramp provider

The minimum viable implementation:

**1.** Create `pkg/payment/<provider>/` and implement the provider's protocol
client (HTTP, gRPC, whatever it speaks).

**2.** Define your `Options` and result `Payload` types in that package:

```go
package fooramp

import "github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"

const ProviderID offramp.ProviderID = "fooramp"

type Options struct {
    SomeFooSpecificFlag string
}
func (Options) ProviderID() offramp.ProviderID { return ProviderID }

type Payload struct {
    FooReceiptID string
}
func (Payload) ProviderID() offramp.ProviderID { return ProviderID }
```

Add the constant to `pkg/payment/offramp/offramp.go` if you want
auto-completion to surface it. It's just a `ProviderID(string)` — no enum
machinery.

**3.** Build an adapter that implements `offramp.Provider`:

```go
// pkg/mobile/ussd/adapters/offramp_fooramp.go (or wherever the channel glue lives)

type FooRampAdapter struct { /* client, logger, … */ }

func (a *FooRampAdapter) ID() offramp.ProviderID { return fooramp.ProviderID }

func (a *FooRampAdapter) Initiate(ctx context.Context, req offramp.Request) (*offramp.Result, error) {
    opts, err := readFooOptions(req.Options) // type-assert helper
    if err != nil {
        return nil, err
    }
    // ... talk to the provider, then:
    return &offramp.Result{
        RequestID:        "...",
        Status:           "pending",
        AmountUSD:        req.AmountUSD,
        SettlementMethod: "foo_settlement",
        Provider:         fooramp.Payload{FooReceiptID: "..."},
    }, nil
}
```

**4.** Implement any optional capabilities the provider actually supports
(no fake stubs; if Foo has no MoMo concept, don't implement
`MobileMoneyDirectory`):

```go
var (
    _ offramp.Provider     = (*FooRampAdapter)(nil)
    _ offramp.StatusReader = (*FooRampAdapter)(nil)
    _ offramp.Quoter       = (*FooRampAdapter)(nil)
    // intentionally not implementing MobileMoneyDirectory or BalanceReporter
)
```

**5.** Register at boot:

```go
reg := offramp.NewRegistry()
_ = reg.Register(fooAdapter)
_ = reg.Alias("foo_method", fooramp.ProviderID)
```

That's the whole loop. No edits to `offramp.Request` or `offramp.Result`.

---

## Adding a new Stellar anchor

Multiple Stellar anchors (today: MoneyGram; tomorrow: Settle, Unilink,
whatever) share the SEP-1/9/10/24 + JWT cache + memo machinery in
`pkg/payment/stellaranchor`. To wire a new one:

**1.** Create `pkg/payment/<anchor>/` (sibling of `moneygram/`).

**2.** Embed `*stellaranchor.Client` for the protocol layer:

```go
package settle

import "github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"

type Client struct {
    *stellaranchor.Client
    // settle-specific extras (their REST API, custom auth, etc.)
}

func New(cfg Config) (*Client, error) {
    anchor, err := stellaranchor.New(stellaranchor.Config{
        HomeDomain:        cfg.HomeDomain,
        WebAuthEndpoint:   cfg.WebAuthEndpoint,
        TransferServerURL: cfg.TransferServerURL,
        ServerSigningKey:  cfg.ServerSigningKey,
        NetworkPassphrase: cfg.NetworkPassphrase,
        USDCIssuer:        cfg.USDCIssuer,
        TreasurySecret:    cfg.TreasurySecret,
    })
    if err != nil { return nil, err }
    return &Client{Client: anchor /*, …*/ }, nil
}
```

Now `Client.Token`, `Client.InitiateWithdrawal`, `Client.GetTransaction`,
`Client.TreasuryAddress`, `Client.USDCIssuer` work via embedding.

**3.** Layer your anchor-specific concerns (custom REST APIs, OAuth flows,
FX rate sources) on top.

**4.** Build your `offramp.Provider` adapter as in the previous section.

The split is: **protocol stays in `stellaranchor`; business logic stays in
the anchor package.** If you find yourself copying SEP-* code, push the
generalisation into `stellaranchor` instead.

---

## Consuming the contract

### From USSD

The USSD loan service adapter depends on the **narrow** capabilities it
actually uses:

```go
// credit module: internal/credit/adapters/loan_service_adapter.go

type LoanServiceAdapter struct {
    offRampSvc offramp.Provider // just Initiate
    // …
}

result, err := a.offRampSvc.Initiate(ctx, offramp.Request{
    LoanID:           loanID,
    AmountUSD:        amountUSD,
    DestinationPhone: req.PhoneNumber,
    CountryCode:      req.CountryCode,
    Options: yellowcard.Options{
        SettlementMethod: yellowcard.SettlementMethodDirect,
    },
})
```

The rate service adapter depends only on `Quoter`:

```go
type RateServiceAdapter struct {
    quoter offramp.Quoter
}

rate, err := a.quoter.Quote(ctx, offramp.QuoteRequest{Currency: "KES"})
```

Both adapters get the same YC concrete adapter at the call site in
`cmd/main.go` and the YC adapter satisfies both interfaces.

### From a future delivery channel

The recipe is identical: depend on `offramp.Provider` (and any capabilities
you need), accept a concrete provider via constructor injection, call
`Initiate(ctx, req)`. The delivery channel adapter package doesn't import any USSD code.

### Reading typed result payloads

When you need provider-specific output (e.g. MoneyGram's interactive URL),
type-assert against the payload defined in that provider's package:

```go
result, err := provider.Initiate(ctx, req)
if err != nil { /* … */ }

switch pl := result.Provider.(type) {
case yellowcard.DirectSettlementPayload:
    persistStellarTxHash(pl.StellarTxHash)
case yellowcard.FiatPayload:
    // YC disbursed from its balance — no chain artifacts to persist
case moneygram.CashPickupPayload:
    smsInteractiveURL(pl.InteractiveURL)
}
```

A code path that only renders cross-provider summary data (`result.AmountLocal`,
`result.Status`) ignores `result.Provider` entirely. The point is most
consumers don't need to know which provider ran.

### Polling provider-specific state

Pollers (e.g. `pkg/services/mgpoller`) get the concrete provider client, not
the generic `Provider`. They need provider-specific behaviour (MG's SEP-24
state machine, YC's refund lookup) that doesn't generalise. Don't try to
abstract a poller across providers; let each provider's lifecycle drive its
own poller.

---

## What lives where, and why

| If you're changing…                                | …edit this package                |
|---------------------------------------------------|-----------------------------------|
| The shape of `Request` / `Result` / `Status`       | `pkg/payment/offramp`             |
| A new capability interface                         | `pkg/payment/offramp`             |
| How providers are resolved at runtime              | `pkg/payment/offramp/registry.go` |
| SEP-1/9/10/24 protocol details                     | `pkg/payment/stellaranchor`       |
| MoneyGram REST FX or OAuth                         | `pkg/payment/moneygram`           |
| YellowCard HTTP / HMAC / channel data              | `pkg/payment/yellowcard`          |
| USSD-specific request shaping                      | `pkg/mobile/ussd/adapters`        |

If a change spans two columns, you're probably building the wrong thing.
Examples:

- *"I want to add a YC-specific field to `offramp.Request`."* No, put it
  in `yellowcard.Options` and let the YC adapter read it.
- *"I want a generic `BalanceReporter` that works for every provider."* No, MG is funded per-transaction. Don't force it. Adapters that have a balance
  implement the capability; adapters that don't, don't.
- *"I want the registry to know about USSD."* No, the registry knows about
  `ProviderID` and `PayoutMethod`. The USSD layer maps its menu choices to
  those strings.

When in doubt, ask: *would a future delivery channel need to import this from
this layer?* If the answer is no, you're putting it in the wrong layer.
