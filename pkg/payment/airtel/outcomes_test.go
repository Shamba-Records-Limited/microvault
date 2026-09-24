package airtel

import "testing"

// Every documented code the package can receive is classified. An
// unclassified code silently falling through to the default is the failure
// this guards.
func TestOutcomeFor_DocumentedCodes(t *testing.T) {
	cases := map[string]OutcomeKind{
		CodeCollectionSuccess:      OutcomeSuccess,
		CodeEncryptionSuccess:      OutcomeSuccess,
		CodeKYCSuccess:             OutcomeSuccess,
		CodeAccountOK:              OutcomeSuccess,
		CodeESBSuccess:             OutcomeSuccess,
		CodeCollectionAmbiguous:    OutcomeEnquire,
		CodeCollectionInProcess:    OutcomeEnquire,
		CodeCollectionTimedOut:     OutcomeEnquire,
		CodeRouterTimeout:          OutcomeEnquire,
		CodeESBSomethingWrong:      OutcomeEnquire,
		CodeESBInitiateFailed:      OutcomeEnquire,
		CodeESBValidation:          OutcomeEnquire,
		CodeESBStatusFetch:         OutcomeEnquire,
		CodeESBAmbiguous:           OutcomeEnquire,
		CodeCollectionIncorrectPin: OutcomePayer,
		CodeRouterIncorrectPin:     OutcomePayer,
		CodeCollectionNoPinEntered: OutcomePayer,
		CodeCollectionNoBalance:    OutcomePayer,
		CodeCollectionLimit:        OutcomePayer,
		CodeCollectionInvalidAmt:   OutcomePayer,
		CodeCollectionPayeeBarred:  OutcomePayer,
		CodeCollectionForbidden:    OutcomeCredential,
		CodeRouterBadEncryptedPin:  OutcomeCredential,
		CodeRouterPinValidation:    OutcomeCredential,
		CodeRouterNoWallet:         OutcomePermission,
		CodeRouterNoCountryRoute:   OutcomePermission,
		CodeRouterCountryDenied:    OutcomePermission,
		CodeESBNoVendor:            OutcomePermission,
		CodeEncryptionFailed:       OutcomePermission,
		CodeRouterMissingHeader:    OutcomeConfig,
		CodeRouterBadCountry:       OutcomeConfig,
		CodeRouterBadCurrency:      OutcomeConfig,
		CodeRouterMissingCurrency:  OutcomeConfig,
		CodeESBBadCountry:          OutcomeConfig,
		CodeESBBadCurrency:         OutcomeConfig,
		CodeESBBadMSISDN:           OutcomeConfig,
		CodeESBBadMSISDNLength:     OutcomeConfig,
		CodeCollectionRefused:      OutcomeOperational,
		CodeCollectionDoNotHonor:   OutcomeOperational,
		CodeCollectionNotFound:     OutcomeOperational,
		CodeCollectionExpired:      OutcomeOperational,
		CodeESBFailed:              OutcomeOperational,
		CodeESBNoTransaction:       OutcomeOperational,
		CodeESBDuplicateExtID:      OutcomeOperational,
		CodeKYCNotFound:            OutcomeOperational,
		CodeKYCFailed:              OutcomeOperational,
		CodeAccountNoUsr:           OutcomeOperational,
		CodeAccountFail:            OutcomeOperational,
	}

	for code, want := range cases {
		t.Run(code, func(t *testing.T) {
			got := OutcomeFor(code)
			if got.Kind != want {
				t.Fatalf("OutcomeFor(%q).Kind = %q, want %q", code, got.Kind, want)
			}
			if got.Message == "" {
				t.Fatalf("OutcomeFor(%q) carries no message", code)
			}
		})
	}
}

// An undocumented code must not be read as a failure. Abandoning a payment
// that settled is the expensive mistake; one extra enquiry is the cheap one.
func TestOutcomeFor_UnknownCodeEnquires(t *testing.T) {
	got := OutcomeFor("DP00800009999")
	if got.Kind != OutcomeEnquire {
		t.Fatalf("OutcomeFor(unknown).Kind = %q, want %q", got.Kind, OutcomeEnquire)
	}
	if got.Retryable {
		t.Fatal("an unknown outcome must not be retryable")
	}
}

// No outcome that needs an enquiry may also be marked retryable: resending
// the same id is a duplicate-transaction error, and a fresh id is a second
// payment.
func TestOutcomes_EnquireIsNeverRetryable(t *testing.T) {
	for code, outcome := range outcomes {
		if outcome.Kind == OutcomeEnquire && outcome.Retryable {
			t.Fatalf("%s is classified enquire but marked retryable", code)
		}
	}
}

// The pending set and the enquire classification are two encodings of one
// fact and must not drift apart.
func TestPendingCodesAreEnquireOutcomes(t *testing.T) {
	for code := range pendingCodes {
		if got := OutcomeFor(code); got.Kind != OutcomeEnquire {
			t.Fatalf("%s is pending but classified %q", code, got.Kind)
		}
	}
}
