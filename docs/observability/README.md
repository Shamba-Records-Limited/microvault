# Microvault Observability and Alerting

How logs, metrics and traces leave the services, how ops alerts are raised in
code, and how OpenObserve turns them into Slack, Telegram and PagerDuty
notifications. The stack and its provisioning live in the credit module
(`microvault-credit`); the alert vocabulary lives here in core.

## The stack

| Piece | Role |
|---|---|
| OpenObserve (OSS, one container) | Stores logs, metrics and traces; runs the alert rules. Object storage is RustFS in development and R2 on testnet. |
| OTel Collector | Tails every container's JSON log file, receives OTLP metrics and traces from the services, scrapes host metrics, and forwards everything to OpenObserve's `default` stream. |
| `o2-provision` | One-shot service run on every deploy: applies the alert templates, destinations and rules from the repo. |

The collector parses each Go service's `slog` JSON line into fields:
`msg` becomes `body`, `level` becomes `severity`, the compose service name
becomes `service_name`, and every other attribute becomes a column. Query
those names in OpenObserve, not the raw JSON keys. Metric names have dots
replaced with underscores, and histograms arrive as `_bucket`, `_count` and
`_sum` streams.

## Raising an ops alert in code

An ops alert is something a human must act on. Raise it through
[`pkg/alerts`](../../pkg/alerts/alerts.go), never with an ad-hoc log line:

```go
alerts.Raise(logging.WithLoan(ctx, loanID, loanRef), svc, logger,
    "Vault repay outcome unknown", "Loan ...: what happened and what to do")
```

That emits one canonical line, `level=ERROR msg="ops alert"
alert_subject=… alert_message=…`, plus whatever the context carries
(`loan_id`, `loan_reference`, `trace_id`). The rules match on `body = 'ops
alert'` and route on `alert_subject`.

- **The subject is the routing key.** Keep it stable and low-cardinality;
  put specifics in the message. Renaming a paged subject silently demotes it
  to the digest, which is why a test fails if a listed page subject no longer
  appears in the code.
- **`alerts.Raise` never loses an alert.** A nil or failing `alerts.Service`
  falls back to the log line.
- The old `CRITICAL` log prefix is retired. Plain `ERROR` lines are not
  alerts; they only feed the error-spike digest.

## Tiers and destinations

| Tier | Destinations | When |
|---|---|---|
| **page** | Slack page channel, Telegram, PagerDuty | Money at risk or the platform down. Never suppressed. `ops_page` checks every minute and notifies once per subject and loan per 30 minutes. |
| **digest** | Slack digest channel, Telegram | Everything else, hourly 07:00–22:00 EAT; the overnight digest arrives at 06:00. |

A subject pages if it is listed in `deploy/openobserve/<tree>/page-subjects.json`
(credit repo); any other ops alert goes to the digest. To promote a subject,
add it there in **both** trees. Beyond the two ops-alert rules, the trees
carry rules on logs (`panic`, `error_spike`, `credit_silent`,
`account_creation_burst`) and metrics (HTTP 5xx and latency, partner 5xx,
Soroban failures, disbursement failures, repayment expiries, MoneyGram
payout drift, host memory and disk).

## Provisioning: `o2-provision`

`deploy/openobserve/{testnet,mainnet}/` in the credit repo is the source of
truth. On each deploy `o2-provision`:

1. expands `${VAR}` / `${VAR:-default}` placeholders from the environment
   (webhook URLs and tokens exist only there) and reports every missing
   variable at once;
2. upserts templates and destinations by name;
3. ingests one marker record per stream in `schema-seed.json`, because
   OpenObserve rejects a query that names a field it has never ingested;
4. upserts the alerts in the `microvault` folder by name. An alert whose
   metric stream has no data yet is **deferred**, not failed, and is created
   by a later deploy. Alerts in the folder that the tree does not define are
   reported and left alone.

The image is built in CI and pinned on deploy. `-check` validates a tree
offline. When a new alert queries a new log field, add the field to
`schema-seed.json`.

### Secrets (set in each Coolify resource)

`ALERT_SLACK_PAGE_WEBHOOK_URL`, `ALERT_SLACK_DIGEST_WEBHOOK_URL`,
`ALERT_TELEGRAM_BOT_TOKEN`, `ALERT_TELEGRAM_CHAT_ID`,
`ALERT_PAGERDUTY_ROUTING_KEY`. Testnet and mainnet use the same names with
their own values; `o2-provision` will not start without them. In local
development every destination points at the `alert-sink` echo container and
cannot be overridden, so dev alerts never reach real channels.

### Testing the pipeline

```bash
export ZO_ROOT_USER_EMAIL=… ZO_ROOT_USER_PASSWORD=…
make alert-test                 # page tier (default)
make alert-test TIER=all        # page and digest
```

Run it from `microvault-credit`. It writes synthetic `ops alert` lines
(`Alert pipeline test`, a listed page subject, and `Alert pipeline test
(digest)`) so the real rules fire. The page tier arrives in about two minutes
and opens a real PagerDuty incident to resolve; the digest arrives at the
next top of the hour. It defaults to `http://localhost:5080`: your local
stack, or testnet with the tunnel below open.

## Reaching OpenObserve on testnet

OpenObserve listens on `127.0.0.1:5080` on the VPS only; there is no public
route. Tunnel in:

```bash
ssh -N -L 5080:127.0.0.1:5080 <user>@<vps-host>
```

Then open <http://localhost:5080> and sign in with the resource's
`ZO_ROOT_USER_EMAIL` / `ZO_ROOT_USER_PASSWORD`. Alert notifications link to
`http://localhost:5080/...`, so they open directly while the tunnel is up.

## Operator commands

`account-heal`, `mpesa-settle` and `pilot-users` ship in the credit image;
run them with `docker exec <credit container> ./<cmd>` on testnet. Locally,
`make heal-account` and `make pilot-*` run them against the development
`.env`.
`vault-backfill` is a core command and is not in the image.

| Command | Does |
|---|---|
| `account-heal --account <id\|address\|phone> [--apply] [--reissue]` (`make heal-account [APPLY=1] [REISSUE=1]`) | Reports on one borrower's child account (stored vs derived address, on-chain existence); `--apply` runs the same heal as the account reconciler. Refuses `conflict` accounts; `--reissue --apply` moves one to the next free index (floored at `ACCOUNT_INDEX_BASE` and the highest recorded index), creates the new address on-chain, and refuses if the user has open loans or the new address is also taken. |
| `mpesa-settle settle <loan-id> [--confirm]` | Writes the vault leg (`repay_for`) for an M-Pesa repayment settled by hand at the OTC desk, once the USDC is verified on the treasury. Amount and borrower come from the loan row. |
| `pilot-users <add\|list\|unlisted\|revoke\|restore\|invite>` (`make pilot-*`) | Manages the pilot access list: approve a phone + national ID, revoke or restore a person (every row for their ID), list registered users the gate would lock out, and send the invite SMS (`--dry-run` prints it). Run `--help` for flags. |
| `vault-backfill [--confirm]` (core) | Finds every address that ever deposited, minted or transferred vault shares and allowlists the missing ones, before allowlist enforcement is switched on. |

## Related docs

- [Compliance](../compliance/README.md), the vault watcher's alerts.
- [Off-Ramp](../offramp/README.md), the vault repay reconciler's alerts.
- [On-Ramp: MoneyGram](../onramp/moneygram.md), the repayment rail's ops playbook.
