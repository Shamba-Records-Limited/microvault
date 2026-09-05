package mpesa

import (
	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Identifier types are three different namespaces that overlap on small
// integers, which is why each has its own Go type. Passing a reversal's 11
// where a query's 4 belongs is a call that succeeds against the wrong thing.

// PartyIdentifierType identifies a party on Transaction Status and Account
// Balance.
type PartyIdentifierType string

// The party identifier types.
const (
	IdentifierMSISDN    PartyIdentifierType = "1"
	IdentifierTillOwner PartyIdentifierType = "2"
	IdentifierShortcode PartyIdentifierType = "4"
)

// ReversalIdentifierType identifies the receiving party on a Reversal, where
// the only accepted value is 11 rather than the 4 used everywhere else.
type ReversalIdentifierType string

// The reversal identifier type.
const ReversalIdentifierShortcode ReversalIdentifierType = "11"

// AsyncAck acknowledges that Daraja accepted an asynchronous request. It says
// nothing about the outcome, which arrives at the ResultURL.
type AsyncAck struct {
	OriginatorConversationID string `json:"OriginatorConversationID"`
	ConversationID           string `json:"ConversationID"`
	ResponseCode             string `json:"ResponseCode"`
	ResponseDescription      string `json:"ResponseDescription"`
}

// Accepted reports whether Daraja took the request.
func (a AsyncAck) Accepted() bool { return a.ResponseCode == "0" }

// AsyncURLs are the two callbacks every Initiator-bearing endpoint requires.
//
// They must be distinct routes. A queue timeout and a result carry the same
// envelope and cannot be told apart by their body, so the only thing that
// distinguishes them is which URL received the post.
type AsyncURLs struct {
	ResultURL       string
	QueueTimeOutURL string
}

func (u AsyncURLs) validate(errb oopsBuilder) error {
	if u.ResultURL == "" || u.QueueTimeOutURL == "" {
		return errb.
			Code(pkgErrors.CodeMissingDependency).
			Errorf("both a result URL and a queue timeout URL are required")
	}
	if u.ResultURL == u.QueueTimeOutURL {
		return errb.
			Code(pkgErrors.CodeBuildFailed).
			Hint("A timeout and a result carry the same envelope; sharing one URL makes them indistinguishable, and a timeout read as a failure is how a retry moves money twice.").
			Errorf("result and queue timeout URLs must differ")
	}
	for _, url := range []string{u.ResultURL, u.QueueTimeOutURL} {
		if err := AssertCallbackURL(url); err != nil {
			return err
		}
	}
	return nil
}

// validateRemarks enforces the 2-100 character range Safaricom documents.
func validateRemarks(errb oopsBuilder, remarks string) error {
	if len(remarks) < 2 || len(remarks) > 100 {
		return errb.
			Code(pkgErrors.CodeBuildFailed).
			With("length", len(remarks)).
			Errorf("remarks must be between 2 and 100 characters")
	}
	return nil
}
