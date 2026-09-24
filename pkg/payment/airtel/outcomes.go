package airtel

// OutcomeKind is the class of a response code, so an operator can act on it
// without reading Airtel's description.
type OutcomeKind string

// The outcome kinds.
const (
	// OutcomeSuccess is a completed transaction.
	OutcomeSuccess OutcomeKind = "success"

	// OutcomeEnquire is the class Daraja's vocabulary has no word for: the
	// transaction may yet succeed, and the documented response is to ask
	// again with the same id. Retrying the request is how money moves twice.
	OutcomeEnquire OutcomeKind = "enquire"

	// OutcomePayer is a condition on the payer's side — wrong PIN, no PIN
	// entered, no balance, a wallet limit. Nothing we can fix, and nothing a
	// retry of the same request resolves.
	OutcomePayer OutcomeKind = "payer"

	// OutcomeConfig is a request we built wrong. It never succeeds by
	// retrying unchanged.
	OutcomeConfig OutcomeKind = "config"

	// OutcomeCredential is our signing or encryption being wrong — a
	// signature mismatch, a rejected encrypted PIN. It stops every signed
	// endpoint at once and is never the payer's fault.
	OutcomeCredential OutcomeKind = "credential"

	// OutcomePermission is a missing subscription or an unprovisioned
	// country. Granted out of band, never resolved by retrying.
	OutcomePermission OutcomeKind = "permission"

	// OutcomeOperational is a real condition on the transaction — expired,
	// not found, refused, already submitted.
	OutcomeOperational OutcomeKind = "operational"

	// OutcomeTransient is throttling or overload. Back off and retry.
	OutcomeTransient OutcomeKind = "transient"
)

// Outcome is what a response code means to an operator.
type Outcome struct {
	Kind OutcomeKind

	// Retryable reports whether the identical request may be resent. It is
	// false for every OutcomeEnquire code: those need an enquiry with the
	// same id, which is not a retry.
	Retryable bool

	// Message is a short operator-facing summary.
	Message string
}

// ShouldEnquire reports whether the caller must resolve this by asking rather
// than acting.
func (o Outcome) ShouldEnquire() bool { return o.Kind == OutcomeEnquire }

// outcomes maps every documented code this package can receive. The three
// layers share one table because a caller holding a response code does not
// know, and should not have to know, which layer produced it.
var outcomes = map[string]Outcome{
	CodeCollectionSuccess: {Kind: OutcomeSuccess, Message: "Transaction successful."},
	CodeEncryptionSuccess: {Kind: OutcomeSuccess, Message: "Encryption key fetched."},
	CodeKYCSuccess:        {Kind: OutcomeSuccess, Message: "User found."},
	CodeAccountOK:         {Kind: OutcomeSuccess, Message: "Balance fetched."},
	CodeESBSuccess:        {Kind: OutcomeSuccess, Message: "Processed successfully."},

	CodeCollectionAmbiguous: {Kind: OutcomeEnquire, Message: "Still processing. Run an enquiry; do not resend."},
	CodeCollectionInProcess: {Kind: OutcomeEnquire, Message: "Pending. Run an enquiry; do not resend."},
	CodeCollectionTimedOut:  {Kind: OutcomeEnquire, Message: "Timed out at Airtel. The transaction may have completed — enquire."},
	CodeRouterTimeout:       {Kind: OutcomeEnquire, Message: "Gateway timeout. For payments and refunds, enquire rather than retry."},
	CodeESBSomethingWrong:   {Kind: OutcomeEnquire, Message: "Airtel reported an unclassified failure that may be ambiguous. Enquire."},
	CodeESBInitiateFailed:   {Kind: OutcomeEnquire, Message: "Payment initiation failed and may be ambiguous. Enquire."},
	CodeESBValidation:       {Kind: OutcomeEnquire, Message: "Field validation failed and may be ambiguous. Enquire."},
	CodeESBStatusFetch:      {Kind: OutcomeEnquire, Message: "Could not fetch the transaction status. Enquire again later."},
	CodeESBAmbiguous:        {Kind: OutcomeEnquire, Message: "Possibly ambiguous. Check the response code or enquire."},

	CodeCollectionIncorrectPin: {Kind: OutcomePayer, Message: "The payer entered the wrong PIN."},
	CodeRouterIncorrectPin:     {Kind: OutcomePayer, Message: "The payer entered the wrong PIN."},
	CodeCollectionNoPinEntered: {Kind: OutcomePayer, Message: "The payer never entered a PIN; the prompt lapsed."},
	CodeCollectionNoBalance:    {Kind: OutcomePayer, Message: "The payer's wallet cannot cover the amount."},
	CodeCollectionLimit:        {Kind: OutcomePayer, Message: "The payer's wallet transaction limit was exceeded."},
	CodeCollectionInvalidAmt:   {Kind: OutcomePayer, Message: "The amount is below Airtel's minimum."},
	CodeCollectionPayeeBarred:  {Kind: OutcomePayer, Message: "The payee is churned, barred, or not on Airtel Money."},

	CodeCollectionForbidden:   {Kind: OutcomeCredential, Message: "Signature mismatch. Our x-signature did not match the payload we sent."},
	CodeRouterBadEncryptedPin: {Kind: OutcomeCredential, Message: "The encrypted PIN was rejected. Our RSA encryption is wrong, not the PIN."},
	CodeRouterPinValidation:   {Kind: OutcomeCredential, Message: "The gateway errored validating the PIN."},

	CodeRouterNoWallet:       {Kind: OutcomePermission, Message: "No application wallet is configured."},
	CodeRouterNoCountryRoute: {Kind: OutcomePermission, Message: "No country route is configured. Contact Airtel support."},
	CodeRouterCountryDenied:  {Kind: OutcomePermission, Message: "Not authorised to operate in this country."},
	CodeESBNoVendor:          {Kind: OutcomePermission, Message: "No vendor is configured for this country."},
	CodeEncryptionFailed:     {Kind: OutcomePermission, Message: "Could not fetch an encryption key; check the product subscription."},

	CodeRouterMissingHeader:   {Kind: OutcomeConfig, Message: "A mandatory header or body field is missing. Check the country and currency header casing."},
	CodeRouterBadCountry:      {Kind: OutcomeConfig, Message: "Invalid country code."},
	CodeRouterBadCurrency:     {Kind: OutcomeConfig, Message: "Invalid currency code."},
	CodeRouterMissingCurrency: {Kind: OutcomeConfig, Message: "Missing or invalid currency in the request."},
	CodeESBBadCountry:         {Kind: OutcomeConfig, Message: "Invalid country name."},
	CodeESBBadCurrency:        {Kind: OutcomeConfig, Message: "Invalid currency code."},
	CodeESBBadMSISDN:          {Kind: OutcomeConfig, Message: "Invalid MSISDN. Collection takes the national form with no country code."},
	CodeESBBadMSISDNLength:    {Kind: OutcomeConfig, Message: "Invalid MSISDN length."},

	CodeCollectionRefused:    {Kind: OutcomeOperational, Message: "Airtel refused the transaction."},
	CodeCollectionDoNotHonor: {Kind: OutcomeOperational, Message: "Do not honour. Several possible causes; Airtel gives no detail."},
	CodeCollectionNotFound:   {Kind: OutcomeOperational, Message: "No such transaction."},
	CodeCollectionExpired:    {Kind: OutcomeOperational, Message: "The transaction expired before the payer acted."},
	CodeESBFailed:            {Kind: OutcomeOperational, Message: "Failed."},
	CodeESBNoTransaction:     {Kind: OutcomeOperational, Message: "No transaction found with that id."},
	CodeESBDuplicateExtID:    {Kind: OutcomeOperational, Message: "This transaction id already exists at Airtel. Enquire for the original."},
	CodeKYCNotFound:          {Kind: OutcomeOperational, Message: "No Airtel Money user for that MSISDN."},
	CodeKYCFailed:            {Kind: OutcomeOperational, Message: "The user lookup failed."},
	CodeAccountNoUsr:         {Kind: OutcomeOperational, Message: "No Airtel Money user for that MSISDN."},
	CodeAccountFail:          {Kind: OutcomeOperational, Message: "The balance lookup failed."},
}

// OutcomeFor classifies a response code.
//
// An undocumented code falls back to enquire rather than to failure. Airtel's
// own catalogue documents a code family for one product that its samples then
// contradict, and the cost of the two mistakes is not symmetric: treating an
// unknown outcome as failed can abandon a payment that settled, while treating
// it as unresolved only costs one more enquiry.
func OutcomeFor(code string) Outcome {
	if outcome, ok := outcomes[code]; ok {
		return outcome
	}
	return Outcome{
		Kind:    OutcomeEnquire,
		Message: "Airtel reported an undocumented code. Enquire rather than assuming failure.",
	}
}

// Outcome classifies this response's code.
func (r PaymentResponse) Outcome() Outcome { return OutcomeFor(r.ResponseCode()) }
