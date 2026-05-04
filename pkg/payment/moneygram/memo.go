package moneygram

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"
)

// ChildAccountMemo derives a deterministic positive int64 memo from the
// custodial treasury's public key and a user's child-account derivation index.
// The result is used as the SEP-10 memo (and SEP-24 withdraw memo) so that a
// single treasury Stellar account can host many users without their JWTs or
// in-flight transactions cross-contaminating.
//
// Properties:
//   - Deterministic across runs and processes.
//   - Treasury-scoped: testnet/preview/mainnet treasuries produce disjoint memo spaces.
//   - Fits Stellar MEMO_ID and MoneyGram's "≤64-bit positive integer" constraint.
//
// Collision probability at 10^9 users on a 63-bit space (the high bit is
// masked off to keep the value positive) is ≈ 5×10^-11. A collision causes
// JWT scope mis-routing — log loudly and resolve manually if observed.
func ChildAccountMemo(treasuryPubkey string, accountIndex uint32) int64 {
	h := sha256.Sum256([]byte(treasuryPubkey + ":" + strconv.FormatUint(uint64(accountIndex), 10)))
	raw := binary.BigEndian.Uint64(h[:8])
	return int64(raw &^ (uint64(1) << 63))
}
