package soroban

import (
	"time"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// VaultEventKind names the vault contract events the compliance watcher
// cares about. Any other event name (borrowed, repaid, yield_bumped, the
// admin *Updated events, ...) is not an error — DecodeVaultEvent reports it
// as VaultEventUnrecognized and the caller skips it.
type VaultEventKind string

const (
	// VaultEventDeposit covers both deposit and mint: the vault's own
	// OpenZeppelin Vault module emits the same "deposit" event for either,
	// topics [operator, from, receiver].
	VaultEventDeposit        VaultEventKind = "deposit"
	VaultEventTransfer       VaultEventKind = "transfer"
	VaultEventUserAllowed    VaultEventKind = "user_allowed"
	VaultEventUserDisallowed VaultEventKind = "user_disallowed"
	// VaultEventWithdraw covers withdraw and redeem, topics [operator,
	// receiver, owner].
	VaultEventWithdraw VaultEventKind = "withdraw"
	// VaultEventExitDeadlineSet carries the deadline and frozen flag in
	// VaultEvent; topic [account].
	VaultEventExitDeadlineSet     VaultEventKind = "exit_deadline_set"
	VaultEventExitDeadlineCleared VaultEventKind = "exit_deadline_cleared"
	VaultEventUnrecognized        VaultEventKind = ""
)

// VaultEvent is the decoded subset of a vault contract event the watcher
// needs: which addresses participated, not the full payload. Addresses are
// in topic order (see the const doc comments above for what each kind's
// addresses mean).
type VaultEvent struct {
	Kind      VaultEventKind
	Addresses []string

	// Deadline and Frozen are set for VaultEventExitDeadlineSet only.
	Deadline time.Time
	Frozen   bool
}

// DecodeVaultEvent decodes the topics of a getEvents EventInfo emitted by
// the vault contract. Only the topics are used — deposit's assets/shares and
// so on live in the data payload, which the watcher does not need, since it
// only checks whether the participating addresses were allowlisted.
func DecodeVaultEvent(info protocol.EventInfo) (VaultEvent, error) {
	errb := decodeErr("vault_event").With("event_id", info.ID)

	topics, err := decodeTopics(info.TopicXDR)
	if err != nil {
		return VaultEvent{}, errb.Wrapf(err, "could not decode event topics")
	}
	if len(topics) == 0 || topics[0].Type != xdr.ScValTypeScvSymbol {
		return VaultEvent{}, nil
	}

	kind := VaultEventKind(*topics[0].Sym)
	var addrCount int
	switch kind {
	case VaultEventDeposit, VaultEventWithdraw:
		addrCount = 3
	case VaultEventTransfer:
		addrCount = 2
	case VaultEventUserAllowed, VaultEventUserDisallowed, VaultEventExitDeadlineSet, VaultEventExitDeadlineCleared:
		addrCount = 1
	default:
		return VaultEvent{}, nil
	}
	if len(topics) < 1+addrCount {
		return VaultEvent{}, errb.With("kind", string(kind)).
			With("topic_count", len(topics)).
			Errorf("fewer topics than the event kind requires")
	}

	addresses := make([]string, 0, addrCount)
	for i := 1; i <= addrCount; i++ {
		addr, err := scValToAddress(topics[i])
		if err != nil {
			return VaultEvent{}, errb.With("kind", string(kind)).With("topic_index", i).
				Wrapf(err, "could not decode a participant address")
		}
		addresses = append(addresses, addr)
	}

	event := VaultEvent{Kind: kind, Addresses: addresses}
	if kind == VaultEventExitDeadlineSet {
		if event.Deadline, event.Frozen, err = decodeExitDeadline(info.ValueXDR); err != nil {
			return VaultEvent{}, errb.With("kind", string(kind)).Wrapf(err, "could not decode the exit deadline")
		}
	}
	return event, nil
}

// decodeExitDeadline reads the {deadline, frozen} data map of an
// exit_deadline_set event.
func decodeExitDeadline(valueXDR string) (time.Time, bool, error) {
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(valueXDR, &val); err != nil {
		return time.Time{}, false, err
	}
	m, ok := val.GetMap()
	if !ok || m == nil {
		return time.Time{}, false, decodeErr("exit_deadline").With("got_type", val.Type.String()).Errorf("expected a map")
	}
	var deadline *uint64
	var frozen *bool
	for _, entry := range *m {
		if entry.Key.Type != xdr.ScValTypeScvSymbol {
			continue
		}
		switch string(*entry.Key.Sym) {
		case "deadline":
			v, err := scValToU64(entry.Val)
			if err != nil {
				return time.Time{}, false, err
			}
			deadline = &v
		case "frozen":
			v, err := scValToBool(entry.Val)
			if err != nil {
				return time.Time{}, false, err
			}
			frozen = &v
		}
	}
	if deadline == nil || frozen == nil {
		return time.Time{}, false, decodeErr("exit_deadline").Errorf("deadline or frozen missing")
	}
	return time.Unix(int64(*deadline), 0).UTC(), *frozen, nil
}

// decodeTopics decodes a getEvents EventInfo's base64 XDR topic strings.
func decodeTopics(topicXDR []string) ([]xdr.ScVal, error) {
	topics := make([]xdr.ScVal, 0, len(topicXDR))
	for i, raw := range topicXDR {
		var val xdr.ScVal
		if err := xdr.SafeUnmarshalBase64(raw, &val); err != nil {
			return nil, decodeErr("decode_topic").
				Code(pkgErrors.CodeDecodeFailed).
				With("topic_index", i).
				Wrapf(err, "could not decode topic XDR")
		}
		topics = append(topics, val)
	}
	return topics, nil
}
