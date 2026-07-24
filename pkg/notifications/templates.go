package notifications

// LoanTemplates contains format strings for each loan lifecycle notification.
// Each template uses fmt.Sprintf-style verbs; the exact arguments are
// documented per-field.
type LoanTemplates struct {
	// Approved: args = (DisplayCurrency, DisplayAmount, LoanReference)
	Approved string
	// Rejected: args = (DisplayCurrency, DisplayAmount, Reason)
	Rejected string
	// Disbursed: args = (DisplayCurrency, DisplayAmount, LoanReference)
	Disbursed string
	// Failed: args = (LoanReference)
	Failed string
	// OffRampFailed: args = (DisplayCurrency, DisplayAmount, LoanReference).
	// Sent when vault borrow succeeded but the off-ramp could not be
	// initiated/completed and the USDC has been returned to the vault. The
	// borrower owes nothing — distinct from the credit-default "Failed".
	OffRampFailed string
	// CashPickupApproved: args = (DisplayCurrency, DisplayAmount, LoanReference).
	// Sent when a cash-pickup loan is approved, in place of "Approved" — the
	// generic copy implies a push disbursement, which is misleading here. A
	// second SMS with the MoneyGram interactive URL follows once the off-ramp
	// is initiated (see CashPickupInitiated).
	CashPickupApproved string
	// RepaymentReceived: args = (DisplayCurrency, DisplayAmount, LoanReference, DisplayCurrency, RemainingBalance)
	RepaymentReceived string
	// RepaymentOverdue: args = (DisplayCurrency, DisplayAmount, LoanReference)
	RepaymentOverdue string
	// RepaymentSoon: args = (DisplayCurrency, DisplayAmount, daysUntilDue, LoanReference)
	RepaymentSoon string
	// RepaymentUpcoming: args = (DisplayCurrency, DisplayAmount, dueDateFormatted, LoanReference)
	RepaymentUpcoming string
	// CashPickupInitiated: args = (LoanReference, InteractiveURL)
	CashPickupInitiated string
	// CashPickupReady: args = (DisplayCurrency, DisplayAmount, CashPickupRef, LoanReference, CashPickupInfoURL)
	// Runs to two SMS segments; the support link is worth the second.
	CashPickupReady string
	// CashPickupCancelled: args = (LoanReference)
	//
	// Sent when MoneyGram refunds a cash-pickup loan, which usually means the
	// borrower cancelled in MoneyGram's own app — sometimes by mistake. The
	// wording has to reassure rather than alarm: nothing is owed and they can
	// simply request again.
	CashPickupCancelled string
}

// DefaultLoanTemplates returns ready-to-use loan templates, parameterized for
// any currency.
func DefaultLoanTemplates() *LoanTemplates {
	return &LoanTemplates{
		Approved: "Congratulations! Your loan of %s %.2f has been approved (Ref: %s). " +
			"We are processing it and you will be notified when it's disbursed.",
		Rejected: "Your loan request for %s %.2f was not approved. Reason: %s. " +
			"Dial *384*1234# for more info.",
		Disbursed: "Your loan of %s %.2f has been disbursed (Ref: %s). " +
			"The funds are now available in your account.",
		Failed: "Your loan (Ref: %s) has been marked as defaulted. " +
			"This will affect your credit score. Please contact support.",
		OffRampFailed: "We could not disburse your loan of %s %.2f (Ref: %s). " +
			"No funds left your account and you owe nothing. Please try again or contact support.",
		CashPickupApproved: "Your cash-pickup loan of %s %.2f has been approved (Ref: %s). " +
			"You will receive a verification link shortly to complete pickup at a MoneyGram agent.",
		RepaymentReceived: "Payment of %s %.2f received for loan %s. " +
			"Remaining balance: %s %.2f. Thank you!",
		RepaymentOverdue: "URGENT: Your loan payment of %s %.2f is overdue (Ref: %s). " +
			"Please pay immediately to avoid penalties.",
		RepaymentSoon: "Reminder: Your loan payment of %s %.2f is due in %d days (Ref: %s). " +
			"Dial *384*1234# to pay.",
		RepaymentUpcoming: "Reminder: Your loan payment of %s %.2f is due on %s (Ref: %s).",
		CashPickupInitiated: "Cash-pickup loan (Ref: %s) ready.\n\n" +
			"Open to verify:\n\n%s\n\n" +
			"Final amount shown in the link.",
		CashPickupReady: "Your loan of %s %.2f is ready for pickup at any MoneyGram agent. " +
			"Reference: %s (Loan: %s). Bring valid ID.\nMore info: %s",
		CashPickupCancelled: "Your cash-pickup loan (Ref: %s) was cancelled and the funds returned. " +
			"You owe nothing. Dial *384*1234# to request again.",
	}
}

// swahiliLoanTemplates returns the Swahili loan templates.
func swahiliLoanTemplates() *LoanTemplates {
	return &LoanTemplates{
		Approved: "Hongera! Mkopo wako wa %s %.2f umeidhinishwa (Kumb: %s). " +
			"Tunaushughulikia na utaarifiwa utakapotolewa.",
		Rejected: "Ombi lako la mkopo wa %s %.2f halikuidhinishwa. Sababu: %s. " +
			"Piga *384*1234# kwa maelezo zaidi.",
		Disbursed: "Mkopo wako wa %s %.2f umetolewa (Kumb: %s). " +
			"Fedha sasa zinapatikana kwenye akaunti yako.",
		Failed: "Mkopo wako (Kumb: %s) umewekwa alama ya kutolipwa. " +
			"Hii itaathiri alama yako ya mkopo. Tafadhali wasiliana na msaada.",
		OffRampFailed: "Hatukuweza kutoa mkopo wako wa %s %.2f (Kumb: %s). " +
			"Hakuna fedha zilizotoka kwenye akaunti yako na hudaiwi chochote. Jaribu tena au wasiliana na msaada.",
		CashPickupApproved: "Mkopo wako wa kuchukua pesa wa %s %.2f umeidhinishwa (Kumb: %s). " +
			"Utapokea kiungo cha uthibitisho hivi karibuni kukamilisha uchukuaji kwa wakala wa MoneyGram.",
		RepaymentReceived: "Malipo ya %s %.2f yamepokelewa kwa mkopo %s. " +
			"Salio lililobaki: %s %.2f. Asante!",
		RepaymentOverdue: "HARAKA: Malipo yako ya mkopo ya %s %.2f yamechelewa (Kumb: %s). " +
			"Tafadhali lipa mara moja kuepuka adhabu.",
		RepaymentSoon: "Kumbusho: Malipo yako ya mkopo ya %s %.2f yanastahili kwa siku %d (Kumb: %s). " +
			"Piga *384*1234# kulipa.",
		RepaymentUpcoming: "Kumbusho: Malipo yako ya mkopo ya %s %.2f yanastahili tarehe %s (Kumb: %s).",
		CashPickupInitiated: "Mkopo (Kumb: %s) tayari.\n\n" +
			"Fungua kuthibitisha:\n\n%s\n\n" +
			"Kiasi cha mwisho kwenye kiungo.",
		CashPickupReady: "Mkopo wako wa %s %.2f uko tayari kuchukuliwa kwa wakala yeyote wa MoneyGram. " +
			"Kumbukumbu: %s (Mkopo: %s). Lete kitambulisho halali.\nMaelezo zaidi: %s",
		CashPickupCancelled: "Mkopo wako wa kuchukua fedha (Kumb: %s) umeghairiwa na fedha zimerudishwa. " +
			"Hudaiwi chochote. Piga *384*1234# kuomba tena.",
	}
}

// frenchLoanTemplates returns the French loan templates.
func frenchLoanTemplates() *LoanTemplates {
	return &LoanTemplates{
		Approved: "Félicitations! Votre prêt de %s %.2f a été approuvé (Réf: %s). " +
			"Nous le traitons et vous serez notifié lors du décaissement.",
		Rejected: "Votre demande de prêt de %s %.2f n'a pas été approuvée. Raison: %s. " +
			"Composez *384*1234# pour plus d'informations.",
		Disbursed: "Votre prêt de %s %.2f a été décaissé (Réf: %s). " +
			"Les fonds sont maintenant disponibles sur votre compte.",
		Failed: "Votre prêt (Réf: %s) a été marqué comme défaillant. " +
			"Cela affectera votre score de crédit. Veuillez contacter le support.",
		OffRampFailed: "Nous n'avons pas pu décaisser votre prêt de %s %.2f (Réf: %s). " +
			"Aucun fonds n'a quitté votre compte et vous ne devez rien. Réessayez ou contactez le support.",
		CashPickupApproved: "Votre prêt à retrait en espèces de %s %.2f a été approuvé (Réf: %s). " +
			"Vous recevrez bientôt un lien de vérification pour compléter le retrait chez un agent MoneyGram.",
		RepaymentReceived: "Paiement de %s %.2f reçu pour le prêt %s. " +
			"Solde restant: %s %.2f. Merci!",
		RepaymentOverdue: "URGENT: Votre paiement de prêt de %s %.2f est en retard (Réf: %s). " +
			"Veuillez payer immédiatement pour éviter des pénalités.",
		RepaymentSoon: "Rappel: Votre paiement de prêt de %s %.2f est dû dans %d jours (Réf: %s). " +
			"Composez *384*1234# pour payer.",
		RepaymentUpcoming: "Rappel: Votre paiement de prêt de %s %.2f est dû le %s (Réf: %s).",
		CashPickupInitiated: "Prêt (Réf: %s) prêt.\n\n" +
			"Ouvrez pour vérifier:\n\n%s\n\n" +
			"Montant final dans le lien.",
		CashPickupReady: "Votre prêt de %s %.2f est prêt à être retiré chez tout agent MoneyGram. " +
			"Référence: %s (Prêt: %s). Apportez une pièce d'identité valide.\nPlus d'infos: %s",
		CashPickupCancelled: "Votre prêt à retrait espèces (Réf: %s) a été annulé et les fonds retournés. " +
			"Vous ne devez rien. Composez *384*1234# pour redemander.",
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
