package notifications

// AccountTemplates contains fmt.Sprintf-style format strings for account and
// PIN lifecycle notification messages. Each field documents its expected
// arguments.
type AccountTemplates struct {
	// RegistrationSuccess formats the welcome message after successful
	// registration. Args: (FullName string).
	RegistrationSuccess string

	// RegistrationFailed formats the message when registration cannot be
	// completed. Args: (Reason string).
	RegistrationFailed string

	// WrongAttempt formats the alert sent when an incorrect PIN is entered.
	// Args: (RemainingAttempts int).
	WrongAttempt string

	// AccountLocked formats the security alert sent when the account is
	// locked due to repeated failed PIN attempts. Args: (LockedUntil string).
	AccountLocked string

	// PINChanged formats the confirmation sent after a successful PIN change.
	// No format args.
	PINChanged string

	// PINChangeFailed formats the alert sent when a PIN change attempt fails.
	// Args: (Reason string).
	PINChangeFailed string

	// PINReset formats the confirmation sent after a successful PIN reset via
	// the recovery flow. No format args.
	PINReset string

	// PINResetFailed formats the alert sent when a PIN reset attempt fails.
	// Args: (Reason string).
	PINResetFailed string
}

// DefaultAccountTemplates returns sensible default templates for all account
// and PIN notification messages. Pass nil to NewSMSAccountNotifier to use
// these defaults.
func DefaultAccountTemplates() *AccountTemplates {
	return &AccountTemplates{
		RegistrationSuccess: "Welcome to Shamba Records, %s! " +
			"Your account is active and your PIN is set. " +
			"Dial *789*10# to request your first loan.\n\n" +
			"Next: My Account > PIN Manager > Security Questions - so you can " +
			"recover your account if you lose your phone. " +
			"Add My Details for faster cash pickup.",
		RegistrationFailed: "Your Shamba Records registration could not be completed. " +
			"Reason: %s. Please try again or contact support.",
		WrongAttempt: "ALERT: An incorrect PIN was entered on your Shamba Records account. " +
			"%d attempt(s) remaining before your account is locked.",
		AccountLocked: "SECURITY: Your Shamba Records account has been temporarily locked " +
			"due to multiple failed PIN attempts. Try again in %s or dial to reset your PIN.",
		PINChanged: "Your Shamba Records PIN has been changed successfully. " +
			"If you did not make this change, dial *384*1234# immediately to reset your PIN.",
		PINChangeFailed: "A PIN change attempt on your Shamba Records account was unsuccessful. " +
			"Reason: %s. If this was not you, please reset your PIN immediately.",
		PINReset: "Your Shamba Records PIN has been reset successfully. " +
			"You can now access your account with your new PIN.",
		PINResetFailed: "A PIN reset attempt on your Shamba Records account failed. " +
			"Reason: %s. Please try again or contact support.",
	}
}

// swahiliAccountTemplates returns the Swahili account/PIN templates.
func swahiliAccountTemplates() *AccountTemplates {
	return &AccountTemplates{
		RegistrationSuccess: "Karibu Shamba Records, %s! " +
			"Akaunti yako iko hai na PIN yako imewekwa. " +
			"Piga *384# kuomba mkopo wako wa kwanza.\n\n" +
			"Ifuatayo: Akaunti Yangu > Dhibiti PIN > Maswali ya Usalama - ili " +
			"uweze kurejesha akaunti yako ukipoteza simu. " +
			"Weka Maelezo Yangu kwa kuchukua pesa haraka.",
		RegistrationFailed: "Usajili wako wa Shamba Records haukukamilika. " +
			"Sababu: %s. Tafadhali jaribu tena au wasiliana na msaada.",
		WrongAttempt: "TAHADHARI: PIN isiyo sahihi iliwekwa kwenye akaunti yako ya Shamba Records. " +
			"Majaribio %d yamebaki kabla akaunti yako kufungwa.",
		AccountLocked: "USALAMA: Akaunti yako ya Shamba Records imefungwa kwa muda " +
			"kutokana na majaribio mengi ya PIN yaliyoshindwa. Jaribu tena baada ya %s au piga kuweka upya PIN.",
		PINChanged: "PIN yako ya Shamba Records imebadilishwa. " +
			"Kama hukufanya mabadiliko haya, piga *384*1234# mara moja kuweka upya PIN yako.",
		PINChangeFailed: "Jaribio la kubadilisha PIN kwenye akaunti yako ya Shamba Records halikufanikiwa. " +
			"Sababu: %s. Kama hukuwa wewe, tafadhali weka upya PIN yako mara moja.",
		PINReset: "PIN yako ya Shamba Records imewekwa upya. " +
			"Sasa unaweza kufikia akaunti yako kwa PIN yako mpya.",
		PINResetFailed: "Jaribio la kuweka upya PIN kwenye akaunti yako ya Shamba Records limeshindwa. " +
			"Sababu: %s. Tafadhali jaribu tena au wasiliana na msaada.",
	}
}

// frenchAccountTemplates returns the French account/PIN templates.
func frenchAccountTemplates() *AccountTemplates {
	return &AccountTemplates{
		RegistrationSuccess: "Bienvenue à Shamba Records, %s! " +
			"Votre compte est actif et votre PIN est défini. " +
			"Composez *384# pour demander votre premier pret.\n\n" +
			"Ensuite: Mon Compte > Gérer PIN > Questions de Sécurité - pour " +
			"récupérer votre compte si vous perdez votre téléphone. " +
			"Ajoutez Mes Infos pour un retrait plus rapide.",
		RegistrationFailed: "Votre inscription à Shamba Records n'a pas pu etre complétée. " +
			"Raison: %s. Veuillez réessayer ou contacter le support.",
		WrongAttempt: "ALERTE: Un PIN incorrect a été saisi sur votre compte Shamba Records. " +
			"%d tentative(s) restante(s) avant le verrouillage de votre compte.",
		AccountLocked: "SÉCURITÉ: Votre compte Shamba Records a été temporairement verrouillé " +
			"suite à plusieurs échecs de PIN. Réessayez dans %s ou composez pour réinitialiser votre PIN.",
		PINChanged: "Votre PIN Shamba Records a été modifié avec succès. " +
			"Si vous n'etes pas à l'origine de ce changement, composez *384*1234# immédiatement pour réinitialiser votre PIN.",
		PINChangeFailed: "Une tentative de changement de PIN sur votre compte Shamba Records a échoué. " +
			"Raison: %s. Si ce n'était pas vous, réinitialisez votre PIN immédiatement.",
		PINReset: "Votre PIN Shamba Records a été réinitialisé avec succès. " +
			"Vous pouvez maintenant accéder à votre compte avec votre nouveau PIN.",
		PINResetFailed: "Une tentative de réinitialisation de PIN sur votre compte Shamba Records a échoué. " +
			"Raison: %s. Veuillez réessayer ou contacter le support.",
	}
}

// localizedAccountTemplates returns account templates keyed by ISO language code.
func localizedAccountTemplates() map[string]*AccountTemplates {
	return map[string]*AccountTemplates{
		"en": DefaultAccountTemplates(),
		"sw": swahiliAccountTemplates(),
		"fr": frenchAccountTemplates(),
	}
}
