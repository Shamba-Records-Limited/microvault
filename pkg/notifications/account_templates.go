package notifications

import (
	"fmt"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// AccountMessage renders one account or PIN lifecycle notification.
type AccountMessage func(n contracts.AccountNotification) string

// AccountTemplates holds one message renderer per account and PIN lifecycle
// event.
type AccountTemplates struct {
	// RegistrationSuccess welcomes a user after successful registration.
	RegistrationSuccess AccountMessage
	// RegistrationFailed reports that registration could not be completed.
	RegistrationFailed AccountMessage
	// WrongAttempt alerts on an incorrect PIN entry and counts down to lockout.
	WrongAttempt AccountMessage
	// AccountLocked is the security alert sent once repeated failures lock the
	// account.
	AccountLocked AccountMessage
	// PINChanged confirms a successful PIN change.
	PINChanged AccountMessage
	// PINChangeFailed alerts on an unsuccessful PIN change.
	PINChangeFailed AccountMessage
	// PINReset confirms a successful reset through the recovery flow.
	PINReset AccountMessage
	// PINResetFailed alerts on an unsuccessful reset.
	PINResetFailed AccountMessage
}

// DefaultAccountTemplates returns brand-free English account and PIN copy.
func DefaultAccountTemplates() *AccountTemplates {
	return &AccountTemplates{
		RegistrationSuccess: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Welcome, %s! Your account is active and your PIN is set.", n.FullName)
		},
		RegistrationFailed: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Your registration could not be completed. Reason: %s. "+
				"Please try again or contact support.", n.Reason)
		},
		WrongAttempt: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("ALERT: An incorrect PIN was entered on your account. "+
				"%d attempt(s) remaining before your account is locked.", n.RemainingAttempts)
		},
		AccountLocked: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("SECURITY: Your account has been temporarily locked due to multiple "+
				"failed PIN attempts. Try again in %s.", n.LockedUntil)
		},
		PINChanged: func(contracts.AccountNotification) string {
			return "Your PIN has been changed successfully. " +
				"If you did not make this change, reset your PIN immediately."
		},
		PINChangeFailed: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("A PIN change attempt on your account was unsuccessful. Reason: %s. "+
				"If this was not you, please reset your PIN immediately.", n.Reason)
		},
		PINReset: func(contracts.AccountNotification) string {
			return "Your PIN has been reset successfully. You can now access your account with your new PIN."
		},
		PINResetFailed: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("A PIN reset attempt on your account failed. Reason: %s. "+
				"Please try again or contact support.", n.Reason)
		},
	}
}

// swahiliAccountTemplates returns the Swahili account/PIN templates.
func swahiliAccountTemplates() *AccountTemplates {
	return &AccountTemplates{
		RegistrationSuccess: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Karibu, %s! Akaunti yako iko hai na PIN yako imewekwa.", n.FullName)
		},
		RegistrationFailed: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Usajili wako haukukamilika. Sababu: %s. "+
				"Tafadhali jaribu tena au wasiliana na msaada.", n.Reason)
		},
		WrongAttempt: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("TAHADHARI: PIN isiyo sahihi iliwekwa kwenye akaunti yako. "+
				"Majaribio %d yamebaki kabla akaunti yako kufungwa.", n.RemainingAttempts)
		},
		AccountLocked: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("USALAMA: Akaunti yako imefungwa kwa muda kutokana na majaribio mengi "+
				"ya PIN yaliyoshindwa. Jaribu tena baada ya %s.", n.LockedUntil)
		},
		PINChanged: func(contracts.AccountNotification) string {
			return "PIN yako imebadilishwa. Kama hukufanya mabadiliko haya, weka upya PIN yako mara moja."
		},
		PINChangeFailed: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Jaribio la kubadilisha PIN kwenye akaunti yako halikufanikiwa. Sababu: %s. "+
				"Kama hukuwa wewe, tafadhali weka upya PIN yako mara moja.", n.Reason)
		},
		PINReset: func(contracts.AccountNotification) string {
			return "PIN yako imewekwa upya. Sasa unaweza kufikia akaunti yako kwa PIN yako mpya."
		},
		PINResetFailed: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Jaribio la kuweka upya PIN kwenye akaunti yako limeshindwa. Sababu: %s. "+
				"Tafadhali jaribu tena au wasiliana na msaada.", n.Reason)
		},
	}
}

// frenchAccountTemplates returns the French account/PIN templates.
func frenchAccountTemplates() *AccountTemplates {
	return &AccountTemplates{
		RegistrationSuccess: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Bienvenue, %s! Votre compte est actif et votre PIN est défini.", n.FullName)
		},
		RegistrationFailed: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Votre inscription n'a pas pu etre complétée. Raison: %s. "+
				"Veuillez réessayer ou contacter le support.", n.Reason)
		},
		WrongAttempt: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("ALERTE: Un PIN incorrect a été saisi sur votre compte. "+
				"%d tentative(s) restante(s) avant le verrouillage de votre compte.", n.RemainingAttempts)
		},
		AccountLocked: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("SÉCURITÉ: Votre compte a été temporairement verrouillé suite à plusieurs "+
				"échecs de PIN. Réessayez dans %s.", n.LockedUntil)
		},
		PINChanged: func(contracts.AccountNotification) string {
			return "Votre PIN a été modifié avec succès. " +
				"Si vous n'etes pas à l'origine de ce changement, réinitialisez votre PIN immédiatement."
		},
		PINChangeFailed: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Une tentative de changement de PIN sur votre compte a échoué. Raison: %s. "+
				"Si ce n'était pas vous, réinitialisez votre PIN immédiatement.", n.Reason)
		},
		PINReset: func(contracts.AccountNotification) string {
			return "Votre PIN a été réinitialisé avec succès. " +
				"Vous pouvez maintenant accéder à votre compte avec votre nouveau PIN."
		},
		PINResetFailed: func(n contracts.AccountNotification) string {
			return fmt.Sprintf("Une tentative de réinitialisation de PIN sur votre compte a échoué. Raison: %s. "+
				"Veuillez réessayer ou contacter le support.", n.Reason)
		},
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
