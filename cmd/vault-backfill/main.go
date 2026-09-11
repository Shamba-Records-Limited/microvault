// Command vault-backfill finds every address that has ever participated in
// a vault deposit, mint, or transfer, and allowlists whichever of them are
// not already allowed — the backfill the vault's own allowlist_enforced doc
// comment calls for before enforcement is turned on.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	_ "github.com/joho/godotenv/autoload"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/rpc"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/soroban"
)

// ledgerGetter is the one method needed to find a definitely-in-range ledger
// to seed the event scan's probe call.
type ledgerGetter interface {
	GetLatestLedger(ctx context.Context) (protocol.GetLatestLedgerResponse, error)
}

const eventsPageLimit = 1000

func main() {
	confirm := flag.Bool("confirm", false, "submit allow_depositor for every address found unallowlisted")
	flag.Parse()

	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	rpcClient := cfg.Stellar.NewRpcClient()
	ctx := context.Background()

	addresses, oldest, latest, err := scanDepositors(ctx, rpcClient, cfg.Stellar.ContractID)
	if err != nil {
		log.Fatalf("Event scan failed: %v", err)
	}
	fmt.Printf("Scanned ledgers %d-%d (the RPC node's full retained window)\n", oldest, latest)
	fmt.Printf("Found %d unique addresses across deposit/mint/transfer events\n", len(addresses))

	stellarSvc := stellar.NewService(rpcClient, cfg.Stellar.NetworkPassphrase,
		cfg.Stellar.TreasurySecretKey, cfg.Stellar.AdminSecretKey, cfg.Stellar.ContractID, cfg.Stellar.USDCIssuer)

	var missing []string
	for _, addr := range addresses {
		allowed, err := stellarSvc.IsAllowed(ctx, addr)
		if err != nil {
			log.Printf("warning: could not check %s: %v", addr, err)
			continue
		}
		if !allowed {
			missing = append(missing, addr)
		}
	}

	fmt.Printf("%d of %d are not yet allowlisted:\n", len(missing), len(addresses))
	for _, addr := range missing {
		fmt.Println("  " + addr)
	}

	if !*confirm {
		fmt.Println("\nNothing was sent. Re-run with --confirm to allow_depositor each of these.")
		return
	}
	if cfg.Compliance.ComplianceRoleSecretKey == "" {
		log.Fatal("compliance role secret key is not configured")
	}
	if len(missing) == 0 {
		return
	}

	complianceSvc := stellarSvc.WithComplianceRole(cfg.Compliance.ComplianceRoleSecretKey)
	for _, addr := range missing {
		if err := complianceSvc.AllowDepositor(ctx, addr); err != nil {
			log.Printf("FAILED to allow %s: %v", addr, err)
			continue
		}
		fmt.Printf("allowed %s\n", addr)
	}
}

// scanDepositors pages through every event the RPC node has retained for
// contractID via the response cursor (never by stepping ledgers, which can
// silently skip events sharing a ledger with a full page), decodes each
// via soroban.DecodeVaultEvent, and returns the unique set of addresses
// that appeared in a deposit, mint, or transfer.
func scanDepositors(ctx context.Context, client interface {
	rpc.EventsGetter
	ledgerGetter
}, contractID string) (addresses []string, oldest, latest uint32, err error) {
	seen := make(map[string]struct{})

	filters := []protocol.EventFilter{{ContractIDs: []string{contractID}}}

	// The latest ledger is always in range; the probe response's
	// OldestLedger is what actually bounds the retained window, not a
	// value worth guessing.
	ledgerInfo, err := client.GetLatestLedger(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	probe, err := client.GetEvents(ctx, protocol.GetEventsRequest{
		StartLedger: ledgerInfo.Sequence,
		Filters:     filters,
		Pagination:  &protocol.PaginationOptions{Limit: 1},
	})
	if err != nil {
		return nil, 0, 0, err
	}

	req := protocol.GetEventsRequest{
		StartLedger: probe.OldestLedger,
		Filters:     filters,
		Pagination:  &protocol.PaginationOptions{Limit: eventsPageLimit},
	}

	for {
		resp, err := client.GetEvents(ctx, req)
		if err != nil {
			return nil, 0, 0, err
		}
		if oldest == 0 {
			oldest = resp.OldestLedger
		}
		latest = resp.LatestLedger

		for _, info := range resp.Events {
			event, err := soroban.DecodeVaultEvent(info)
			if err != nil {
				log.Printf("warning: could not decode event %s: %v", info.ID, err)
				continue
			}
			for _, addr := range event.Addresses {
				seen[addr] = struct{}{}
			}
		}

		if len(resp.Events) < eventsPageLimit || resp.Cursor == "" {
			break
		}
		cursor, err := protocol.ParseCursor(resp.Cursor)
		if err != nil {
			return nil, 0, 0, err
		}
		req = protocol.GetEventsRequest{
			Filters:    req.Filters,
			Pagination: &protocol.PaginationOptions{Cursor: &cursor, Limit: eventsPageLimit},
		}
	}

	addresses = make([]string, 0, len(seen))
	for addr := range seen {
		addresses = append(addresses, addr)
	}
	return addresses, oldest, latest, nil
}
