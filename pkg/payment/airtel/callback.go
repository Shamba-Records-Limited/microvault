package airtel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Callback is what Airtel POSTs to the partner's configured callback path.
//
// It carries intermediate or final status, and its status_code is limited to
// TS and TF — narrower than the enquiry vocabulary, which also has TA, TIP
// and TE. A TF here is therefore not proof of terminal failure.
type Callback struct {
	Transaction struct {
		ID            string `json:"id"`
		Message       string `json:"message"`
		StatusCode    string `json:"status_code"`
		AirtelMoneyID string `json:"airtel_money_id"`
	} `json:"transaction"`

	// Hash is present when callback authentication is enabled in Application
	// Settings. Empty means either that it is disabled or that someone who is
	// not Airtel posted this.
	Hash string `json:"hash"`
}

// TransactionStatus reports the callback's outcome as a typed value.
func (c Callback) TransactionStatus() TransactionStatus {
	return ParseTransactionStatus(c.Transaction.StatusCode)
}

// Terminal always reports false. A callback is documented as carrying
// intermediate or final status with nothing to tell them apart, so nothing may
// be closed on one; the method exists so that reading it is a deliberate act
// rather than an omission.
func (c Callback) Terminal() bool { return c.TransactionStatus().Terminal(SourceCallback) }

// ParseCallback decodes a callback body.
//
// A body without a transaction id is rejected: every real callback carries
// one, it is the only thing that correlates the notification to a payment we
// initiated, and recording one without it would stage a row that can never be
// attributed.
func ParseCallback(raw []byte) (*Callback, error) {
	errb := airtelErr("parse_callback")

	var cb Callback
	if err := json.Unmarshal(raw, &cb); err != nil {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the callback")
	}
	if cb.Transaction.ID == "" {
		return nil, errb.
			Code(pkgErrors.CodeIncompleteResponse).
			Errorf("the callback carries no transaction id")
	}
	return &cb, nil
}

// HashVariant names which rendering of the body a hash verified against.
type HashVariant string

// The renderings tried. Airtel documents the hash as HmacSHA256 "over the
// callback body" without saying what "the body" is once the hash is a member
// of it, and the plausible readings produce different digests.
//
// The bytes exactly as received are not among them: the hash arrives inside
// the object it would have to cover, so no sender could have computed it.
const (
	// VariantWithoutHash is the body re-encoded with the hash member removed.
	VariantWithoutHash HashVariant = "without_hash"

	// VariantTransaction is the transaction member exactly as received. It
	// needs no re-encoding, so it is the only candidate immune to key
	// ordering, and it is what a sender signing its payload before wrapping
	// it would produce.
	VariantTransaction HashVariant = "transaction"

	// VariantNone is no match.
	VariantNone HashVariant = ""
)

// HashResult is the outcome of verifying a callback's hash.
type HashResult struct {
	Verified bool
	Variant  HashVariant
}

// VerifyCallbackHash recomputes the HmacSHA256 a callback carries.
//
// Every plausible reading of "the callback body" is tried and any match is
// accepted. That is not a weakening: producing a valid digest under any of
// them requires the private key from Application Settings, which is the whole
// point of the mechanism. It does mean the first live callback settles which
// reading Airtel actually uses, and the matching variant is reported so that
// answer can be recorded rather than inferred.
//
// Verification establishes that Airtel sent the callback. It does not
// establish that the payment settled — the enquiry does that.
func VerifyCallbackHash(raw []byte, key string) (HashResult, error) {
	errb := airtelErr("verify_callback_hash")

	if key == "" {
		return HashResult{}, errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "callback hmac key").
			Errorf("no callback hmac key is configured")
	}

	cb, err := ParseCallback(raw)
	if err != nil {
		return HashResult{}, err
	}
	if cb.Hash == "" {
		return HashResult{}, errb.
			Code(pkgErrors.CodeIncompleteResponse).
			Errorf("the callback carries no hash")
	}

	expected, err := base64.StdEncoding.DecodeString(cb.Hash)
	if err != nil {
		return HashResult{}, errb.
			Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "the callback hash is not valid base64")
	}

	stripped, err := stripHash(raw)
	if err != nil {
		return HashResult{Verified: false, Variant: VariantNone}, err
	}
	if hmac.Equal(expected, signHMAC(stripped, key)) {
		return HashResult{Verified: true, Variant: VariantWithoutHash}, nil
	}

	if member, ok := rawMember(raw, "transaction"); ok && hmac.Equal(expected, signHMAC(member, key)) {
		return HashResult{Verified: true, Variant: VariantTransaction}, nil
	}
	return HashResult{Verified: false, Variant: VariantNone}, nil
}

// rawMember returns one top-level member's bytes exactly as they arrived.
func rawMember(raw []byte, name string) ([]byte, bool) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, false
	}
	member, ok := generic[name]
	return member, ok && len(member) > 0
}

// SignCallback produces the hash Airtel would send for a body. It exists for
// the stub and the tests: a verifier that has never seen a valid signature is
// only assumed to work.
func SignCallback(raw []byte, key string) string {
	return base64.StdEncoding.EncodeToString(signHMAC(raw, key))
}

func signHMAC(raw []byte, key string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(raw)
	return mac.Sum(nil)
}

// stripHash re-encodes the body without its hash member. Key order is not
// preserved — encoding/json sorts object keys — so this rendering is only
// meaningful if Airtel computes over a canonical form, which is exactly the
// question the two variants exist to answer.
func stripHash(raw []byte) ([]byte, error) {
	errb := airtelErr("strip_callback_hash")

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the callback")
	}
	delete(generic, "hash")

	encoded, err := json.Marshal(generic)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not re-encode the callback")
	}
	return encoded, nil
}
