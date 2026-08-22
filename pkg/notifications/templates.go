package notifications

import (
	"fmt"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// LoanMessage renders one loan lifecycle notification. Taking the whole
// notification rather than positional arguments lets the compiler check both
// the fields a message reads and the verbs it formats them with.
type LoanMessage func(n contracts.LoanNotification) string

// LoanTemplates holds one message renderer per loan lifecycle event.
//
// The copy here is deliberately brand-free: it names no company, no support
// URL, and no USSD code, because those differ per builder and per deployment.
// Builders supply their own wording through [WithLoanTemplates]; fields left
// nil fall back to these defaults.
type LoanTemplates struct {
	Approved LoanMessage
	Rejected LoanMessage
	// Disbursed announces that fiat has been pushed to the borrower.
	Disbursed LoanMessage
	// Failed is the credit-default notice.
	Failed LoanMessage
	// OffRampFailed is sent when vault borrow succeeded but the off-ramp could
	// not be initiated or completed and the USDC has been returned to the
	// vault. The borrower owes nothing — distinct from the credit-default
	// Failed.
	OffRampFailed LoanMessage
	// CashPickupApproved replaces Approved for cash-pickup loans, whose
	// generic wording implies a push disbursement. A second SMS with the
	// MoneyGram interactive URL follows once the off-ramp is initiated (see
	// CashPickupInitiated).
	CashPickupApproved LoanMessage
	RepaymentReceived  LoanMessage
	// RepaymentOverdue, RepaymentSoon and RepaymentUpcoming are selected by
	// [SMSLoanNotifier.NotifyRepaymentReminder] from the days remaining; use
	// [DaysUntilDue] to render that count.
	RepaymentOverdue  LoanMessage
	RepaymentSoon     LoanMessage
	RepaymentUpcoming LoanMessage
	// CashPickupInitiated carries the MoneyGram interactive URL. The URL is
	// single-use and the payout amount is not yet locked, so the copy must say
	// the final amount appears in the link.
	CashPickupInitiated LoanMessage
	// CashPickupReady carries the reference the borrower quotes at the agent.
	// Runs to two SMS segments; the support link is worth the second. Unlike
	// InteractiveURL, CashPickupInfoURL stays valid after settlement.
	CashPickupReady LoanMessage
	// Statement answers a user-initiated "My Loans" request with one loan's
	// reference, amount and due date. Use [DueDateText] for the date — a loan
	// may have none.
	Statement LoanMessage
	// CashPickupCancelled is sent when MoneyGram refunds a cash-pickup loan,
	// which usually means the borrower cancelled in MoneyGram's own app —
	// sometimes by mistake. The wording has to reassure rather than alarm:
	// nothing is owed and they can simply request again.
	CashPickupCancelled LoanMessage
}

// DueDateText renders a notification's due date as YYYY-MM-DD, or "-" when the
// loan has none. Templates must not format DueDate directly: it is a pointer
// and is nil until a loan is disbursed.
func DueDateText(n contracts.LoanNotification) string {
	if n.DueDate == nil {
		return "-"
	}
	return n.DueDate.Format("2006-01-02")
}

// DaysUntilDue returns whole days between now and the notification's due date,
// negative once overdue and zero when no due date is set.
func DaysUntilDue(n contracts.LoanNotification) int {
	if n.DueDate == nil {
		return 0
	}
	return int(time.Until(*n.DueDate).Hours() / 24)
}

// DefaultLoanTemplates returns brand-free English loan copy.
func DefaultLoanTemplates() *LoanTemplates {
	return &LoanTemplates{
		Approved: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Congratulations! Your loan of %s %.2f has been approved (Ref: %s). "+
				"We are processing it and you will be notified when it's disbursed.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		Rejected: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Your loan request for %s %.2f was not approved. Reason: %s.",
				n.DisplayCurrency, n.DisplayAmount, n.Reason)
		},
		Disbursed: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Your loan of %s %.2f has been disbursed (Ref: %s). "+
				"The funds are now available in your mobile money account.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		Failed: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Your loan (Ref: %s) has been marked as defaulted. "+
				"This will affect your credit score. Please contact support.",
				n.LoanReference)
		},
		OffRampFailed: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("We could not disburse your loan of %s %.2f (Ref: %s). "+
				"No funds left your account and you owe nothing. Please try again or contact support.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		CashPickupApproved: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Congratulations! Your cash-pickup loan of %s %.2f has been approved (Ref: %s). "+
				"You will receive a verification link shortly to complete pickup at a MoneyGram agent.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		RepaymentReceived: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Payment of %s %.2f received for loan %s. Remaining balance: %s %.2f. Thank you!",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference, n.DisplayCurrency, n.RemainingBalance)
		},
		RepaymentOverdue: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("URGENT: Your loan payment of %s %.2f is overdue (Ref: %s). "+
				"Please pay immediately to avoid penalties.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		RepaymentSoon: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Reminder: Your loan payment of %s %.2f is due in %d days (Ref: %s).",
				n.DisplayCurrency, n.DisplayAmount, DaysUntilDue(n), n.LoanReference)
		},
		RepaymentUpcoming: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Reminder: Your loan payment of %s %.2f is due on %s (Ref: %s).",
				n.DisplayCurrency, n.DisplayAmount, n.DueDate.Format("2006-01-02"), n.LoanReference)
		},
		CashPickupInitiated: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Cash-pickup loan (Ref: %s) ready. Final amount shown in the link.\nOpen to verify:\n%s",
				n.LoanReference, n.InteractiveURL)
		},
		CashPickupReady: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Your loan of %s %.2f is ready for pickup at any MoneyGram agent. "+
				"Reference: %s (Loan: %s). Bring valid ID.\nMore info: %s",
				n.DisplayCurrency, n.DisplayAmount, n.CashPickupRef, n.LoanReference, n.CashPickupInfoURL)
		},
		Statement: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Loan %s: %s %.2f, due %s",
				n.LoanReference, n.DisplayCurrency, n.DisplayAmount, DueDateText(n))
		},
		CashPickupCancelled: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Your cash-pickup loan (Ref: %s) was cancelled and the funds returned. You owe nothing.",
				n.LoanReference)
		},
	}
}

// swahiliLoanTemplates returns the Swahili loan templates.
func swahiliLoanTemplates() *LoanTemplates {
	return &LoanTemplates{
		Approved: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Hongera! Mkopo wako wa %s %.2f umeidhinishwa (Kumb: %s). "+
				"Tunaushughulikia na utaarifiwa utakapotolewa.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		Rejected: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Ombi lako la mkopo wa %s %.2f halikuidhinishwa. Sababu: %s.",
				n.DisplayCurrency, n.DisplayAmount, n.Reason)
		},
		Disbursed: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Mkopo wako wa %s %.2f umetolewa (Kumb: %s). "+
				"Fedha sasa zinapatikana kwenye akaunti yako ya pesa za simu.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		Failed: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Mkopo wako (Kumb: %s) umewekwa alama ya kutolipwa. "+
				"Hii itaathiri alama yako ya mkopo. Tafadhali wasiliana na msaada.",
				n.LoanReference)
		},
		OffRampFailed: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Hatukuweza kutoa mkopo wako wa %s %.2f (Kumb: %s). "+
				"Hakuna fedha zilizotoka kwenye akaunti yako na hudaiwi chochote. Jaribu tena au wasiliana na msaada.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		CashPickupApproved: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Hongera! Mkopo wako wa kuchukua pesa wa %s %.2f umeidhinishwa (Kumb: %s). "+
				"Utapokea kiungo cha uthibitisho hivi karibuni kukamilisha uchukuaji kwa wakala wa MoneyGram.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		RepaymentReceived: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Malipo ya %s %.2f yamepokelewa kwa mkopo %s. Salio lililobaki: %s %.2f. Asante!",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference, n.DisplayCurrency, n.RemainingBalance)
		},
		RepaymentOverdue: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("HARAKA: Malipo yako ya mkopo ya %s %.2f yamechelewa (Kumb: %s). "+
				"Tafadhali lipa mara moja kuepuka adhabu.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		RepaymentSoon: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Kumbusho: Malipo yako ya mkopo ya %s %.2f yanastahili kwa siku %d (Kumb: %s).",
				n.DisplayCurrency, n.DisplayAmount, DaysUntilDue(n), n.LoanReference)
		},
		RepaymentUpcoming: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Kumbusho: Malipo yako ya mkopo ya %s %.2f yanastahili tarehe %s (Kumb: %s).",
				n.DisplayCurrency, n.DisplayAmount, n.DueDate.Format("2006-01-02"), n.LoanReference)
		},
		CashPickupInitiated: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Mkopo (Kumb: %s) tayari. Kiasi cha mwisho kwenye kiungo.\nFungua kuthibitisha:\n%s",
				n.LoanReference, n.InteractiveURL)
		},
		CashPickupReady: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Mkopo wako wa %s %.2f uko tayari kuchukuliwa kwa wakala yeyote wa MoneyGram. "+
				"Kumbukumbu: %s (Mkopo: %s). Lete kitambulisho halali.\nMaelezo zaidi: %s",
				n.DisplayCurrency, n.DisplayAmount, n.CashPickupRef, n.LoanReference, n.CashPickupInfoURL)
		},
		Statement: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Mkopo %s: %s %.2f, malipo %s",
				n.LoanReference, n.DisplayCurrency, n.DisplayAmount, DueDateText(n))
		},
		CashPickupCancelled: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Mkopo wako wa kuchukua fedha (Kumb: %s) umeghairiwa na fedha zimerudishwa. Hudaiwi chochote.",
				n.LoanReference)
		},
	}
}

// frenchLoanTemplates returns the French loan templates.
func frenchLoanTemplates() *LoanTemplates {
	return &LoanTemplates{
		Approved: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Félicitations! Votre pret de %s %.2f a été approuvé (Réf: %s). "+
				"Nous le traitons et vous serez notifié lors du décaissement.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		Rejected: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Votre demande de pret de %s %.2f n'a pas été approuvée. Raison: %s.",
				n.DisplayCurrency, n.DisplayAmount, n.Reason)
		},
		Disbursed: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Votre pret de %s %.2f a été décaissé (Réf: %s). "+
				"Les fonds sont maintenant disponibles sur votre compte mobile money.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		Failed: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Votre pret (Réf: %s) a été marqué comme défaillant. "+
				"Cela affectera votre score de crédit. Veuillez contacter le support.",
				n.LoanReference)
		},
		OffRampFailed: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Nous n'avons pas pu décaisser votre pret de %s %.2f (Réf: %s). "+
				"Aucun fonds n'a quitté votre compte et vous ne devez rien. Réessayez ou contactez le support.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		CashPickupApproved: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Félicitations! Votre pret à retrait en espèces de %s %.2f a été approuvé (Réf: %s). "+
				"Vous recevrez bientot un lien de vérification pour compléter le retrait chez un agent MoneyGram.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		RepaymentReceived: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Paiement de %s %.2f recu pour le pret %s. Solde restant: %s %.2f. Merci!",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference, n.DisplayCurrency, n.RemainingBalance)
		},
		RepaymentOverdue: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("URGENT: Votre paiement de pret de %s %.2f est en retard (Réf: %s). "+
				"Veuillez payer immédiatement pour éviter des pénalités.",
				n.DisplayCurrency, n.DisplayAmount, n.LoanReference)
		},
		RepaymentSoon: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Rappel: Votre paiement de pret de %s %.2f est à payer dans %d jours (Réf: %s).",
				n.DisplayCurrency, n.DisplayAmount, DaysUntilDue(n), n.LoanReference)
		},
		RepaymentUpcoming: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Rappel: Votre paiement de pret de %s %.2f est à payer le %s (Réf: %s).",
				n.DisplayCurrency, n.DisplayAmount, n.DueDate.Format("2006-01-02"), n.LoanReference)
		},
		CashPickupInitiated: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Pret (Réf: %s) disponible. Montant final dans le lien.\nOuvrez pour vérifier:\n%s",
				n.LoanReference, n.InteractiveURL)
		},
		CashPickupReady: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Votre pret de %s %.2f est disponible chez tout agent MoneyGram. "+
				"Référence: %s (Pret: %s). Apportez une pièce d'identité valide.\nPlus d'infos: %s",
				n.DisplayCurrency, n.DisplayAmount, n.CashPickupRef, n.LoanReference, n.CashPickupInfoURL)
		},
		Statement: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Pret %s: %s %.2f, echeance %s",
				n.LoanReference, n.DisplayCurrency, n.DisplayAmount, DueDateText(n))
		},
		CashPickupCancelled: func(n contracts.LoanNotification) string {
			return fmt.Sprintf("Votre pret à retrait espèces (Réf: %s) a été annulé et les fonds retournés. Vous ne devez rien.",
				n.LoanReference)
		},
	}
}

// localizedLoanTemplates returns the loan templates keyed by ISO language code.
func localizedLoanTemplates() map[string]*LoanTemplates {
	return map[string]*LoanTemplates{
		"en": DefaultLoanTemplates(),
		"sw": swahiliLoanTemplates(),
		"fr": frenchLoanTemplates(),
	}
}
