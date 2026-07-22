package ussd

import "fmt"

// NewInMemoryLocalizer creates a new in-memory localizer
func NewInMemoryLocalizer(defaultLang string) *InMemoryLocalizer {
	return &InMemoryLocalizer{
		translations: make(map[string]map[string]string),
		defaultLang:  defaultLang,
	}
}

// Get retrieves a localized message
func (l *InMemoryLocalizer) Get(language, key string) string {
	if translations, ok := l.translations[key]; ok {
		if msg, ok := translations[language]; ok {
			return msg
		}
		// Fallback to default language
		if msg, ok := translations[l.defaultLang]; ok {
			return msg
		}
	}
	// Return key if not found
	return key
}

// GetWithDefault retrieves a localized message with a default fallback
func (l *InMemoryLocalizer) GetWithDefault(language, key, defaultMsg string) string {
	if msg := l.Get(language, key); msg != key {
		return msg
	}
	return defaultMsg
}

// HasKey checks if a key exists
func (l *InMemoryLocalizer) HasKey(key string) bool {
	_, ok := l.translations[key]
	return ok
}

// AddTranslation adds a translation
func (l *InMemoryLocalizer) AddTranslation(key, language, message string) {
	if l.translations[key] == nil {
		l.translations[key] = make(map[string]string)
	}
	l.translations[key][language] = message
}

// LoadStandardTranslations loads standard translations
func (l *InMemoryLocalizer) LoadStandardTranslations() {
	translations := map[string]map[string]string{
		"welcome": {
			"en": "Welcome to MicroVaulr!",
			"sw": "Karibu MicroVaulr!",
			"fr": "Bienvenue à MicroVaulr!",
		},
		"goodbye": {
			"en": "Thank you for using MicroVaulr",
			"sw": "Asante kwa kutumia MicroVaulr",
			"fr": "Merci d'utiliser MicroVaulr",
		},
		"error": {
			"en": "An error occurred. Please try again.",
			"sw": "Kosa limetokea. Tafadhali jaribu tena.",
			"fr": "Une erreur s'est produite. Veuillez réessayer.",
		},
		"invalid_input": {
			"en": "Invalid input. Please try again.",
			"sw": "Ingizo batili. Tafadhali jaribu tena.",
			"fr": "Entrée invalide. Veuillez réessayer.",
		},
		"session_expired": {
			"en": "Your session has expired. Please dial again.",
			"sw": "Kipindi chako kimeisha. Tafadhali piga tena.",
			"fr": "Votre session a expiré. Veuillez composer à nouveau.",
		},
		"registration_success": {
			"en": "Registration successful! You can now request loans.",
			"sw": "Usajili umefanikiwa! Sasa unaweza kuomba mikopo.",
			"fr": "Inscription réussie! Vous pouvez maintenant demander des prêts.",
		},
		"loan_request_submitted": {
			"en": "Loan request submitted successfully. You will receive an SMS confirmation.",
			"sw": "Ombi la mkopo limewasilishwa. Utapokea ujumbe wa SMS.",
			"fr": "Demande de prêt soumise avec succès. Vous recevrez un SMS de confirmation.",
		},
		"insufficient_credit": {
			"en": "Your credit score is too low for this loan amount.",
			"sw": "Alama yako ya mkopo ni chini sana kwa kiasi hiki.",
			"fr": "Votre score de crédit est trop bas pour ce montant.",
		},
		"balance": {
			"en": "Your balance:",
			"sw": "Salio lako:",
			"fr": "Votre solde:",
		},
		"no_active_loans": {
			"en": "You have no active loans to repay",
			"sw": "Huna mikopo ya kulipa",
			"fr": "Vous n'avez pas de prêts actifs à rembourser",
		},
		"no_loans": {
			"en": "You have no loans",
			"sw": "Huna mikopo",
			"fr": "Vous n'avez pas de prêts",
		},

		// ── PIN Flow Messages ───────────────────────────────────────
		"pin_enter": {
			"en": "Enter your 4-digit PIN:",
			"sw": "Weka PIN yako ya tarakimu 4:",
			"fr": "Entrez votre PIN à 4 chiffres:",
		},
		"pin_create": {
			"en": "Create a 4-digit PIN:",
			"sw": "Tengeneza PIN ya tarakimu 4:",
			"fr": "Créez un PIN à 4 chiffres:",
		},
		"pin_confirm": {
			"en": "Confirm your PIN:",
			"sw": "Thibitisha PIN yako:",
			"fr": "Confirmez votre PIN:",
		},
		"pin_mismatch": {
			"en": "PINs do not match. Try again.",
			"sw": "PIN hazifanani. Jaribu tena.",
			"fr": "Les PIN ne correspondent pas. Réessayez.",
		},
		"pin_invalid": {
			"en": "PIN must be 4 digits. No repeated (1111) or sequential (1234) numbers.",
			"sw": "PIN lazima iwe tarakimu 4. Isiwe nambari zinazorudiwa au kufuatana.",
			"fr": "Le PIN doit comporter 4 chiffres. Pas de chiffres répétés ou séquentiels.",
		},
		"pin_set_success": {
			"en": "PIN set successfully!",
			"sw": "PIN imewekwa!",
			"fr": "PIN défini avec succès!",
		},
		"pin_changed": {
			"en": "PIN changed successfully.",
			"sw": "PIN imebadilishwa.",
			"fr": "PIN modifié avec succès.",
		},
		"pin_wrong": {
			"en": "Wrong PIN. %d attempt(s) remaining.",
			"sw": "PIN si sahihi. Majaribio %d yamebaki.",
			"fr": "PIN incorrect. %d tentative(s) restante(s).",
		},
		"pin_locked": {
			"en": "Account locked. Try again after %s.",
			"sw": "Akaunti imefungwa. Jaribu tena baada ya %s.",
			"fr": "Compte verrouillé. Réessayez après %s.",
		},
		"pin_weak": {
			"en": "PIN too weak. Avoid 1111, 1234, etc.",
			"sw": "PIN dhaifu sana. Epuka 1111, 1234, nk.",
			"fr": "PIN trop faible. Évitez 1111, 1234, etc.",
		},

		// ── Security Questions ──────────────────────────────────────
		"security_q_setup": {
			"en": "Set up security questions for PIN recovery.",
			"sw": "Weka maswali ya usalama kwa kupata PIN.",
			"fr": "Configurer les questions de sécurité pour la récupération du PIN.",
		},
		"security_q_success": {
			"en": "Security questions saved.",
			"sw": "Maswali ya usalama yamehifadhiwa.",
			"fr": "Questions de sécurité enregistrées.",
		},
		"security_q_answer_prompt": {
			"en": "Enter your answer:",
			"sw": "Weka jibu lako:",
			"fr": "Entrez votre réponse:",
		},

		// ── PIN Recovery ────────────────────────────────────────────
		"recovery_id_prompt": {
			"en": "Enter your national ID to verify:",
			"sw": "Weka kitambulisho chako kuthibitisha:",
			"fr": "Entrez votre pièce d'identité pour vérifier:",
		},
		"recovery_id_wrong": {
			"en": "National ID does not match our records.",
			"sw": "Kitambulisho hakifanani na rekodi zetu.",
			"fr": "La pièce d'identité ne correspond pas à nos dossiers.",
		},
		"recovery_success": {
			"en": "Your PIN has been reset. You can now use your new PIN.",
			"sw": "PIN yako imewekwa upya. Sasa unaweza kutumia PIN yako mpya.",
			"fr": "Votre PIN a été réinitialisé. Vous pouvez maintenant utiliser votre nouveau PIN.",
		},
		"recovery_answers_wrong": {
			"en": "Security answer incorrect. PIN reset failed.",
			"sw": "Jibu la usalama si sahihi. Kuweka upya PIN kumeshindikana.",
			"fr": "Réponse de sécurité incorrecte. Réinitialisation du PIN échouée.",
		},
		"recovery_no_questions": {
			"en": "No security questions set. Please contact support to reset your PIN.",
			"sw": "Hakuna maswali ya usalama. Tafadhali wasiliana na msaada kubadilisha PIN yako.",
			"fr": "Aucune question de sécurité définie. Veuillez contacter le support.",
		},

		// ── Registration Completion ─────────────────────────────────
		"registration_complete": {
			"en": "Registration complete! Dial again to start.",
			"sw": "Usajili umekamilika! Piga tena kuanza.",
			"fr": "Inscription terminée! Composez à nouveau pour commencer.",
		},

		// ── Registration Flow ───────────────────────────────────────
		"reg_name_required": {
			"en": "Name is required. Enter your full name:",
			"sw": "Jina linahitajika. Weka jina lako kamili:",
			"fr": "Le nom est requis. Entrez votre nom complet:",
		},
		"reg_enter_national_id": {
			"en": "Enter your national ID:",
			"sw": "Weka kitambulisho chako:",
			"fr": "Entrez votre pièce d'identité:",
		},
		"reg_national_id_required": {
			"en": "National ID is required. Enter your national ID:",
			"sw": "Kitambulisho kinahitajika. Weka kitambulisho chako:",
			"fr": "La pièce d'identité est requise. Entrez votre pièce d'identité:",
		},
		"reg_national_id_taken": {
			"en": "This national ID is already registered. Re-enter, or contact support if this is you:",
			"sw": "Kitambulisho hiki kimeshasajiliwa. Weka tena, au wasiliana na msaada kama ni chako:",
			"fr": "Cette pièce d'identité est déjà enregistrée. Ressaisissez, ou contactez le support si c'est vous:",
		},
		"reg_bio_gate": {
			"en": "Add optional details for faster cash pickup?\n1. Fill bio info\n2. Skip",
			"sw": "Ongeza maelezo ya hiari kwa uchukuaji wa pesa haraka?\n1. Jaza maelezo\n2. Ruka",
			"fr": "Ajouter des détails facultatifs pour un retrait plus rapide?\n1. Remplir les infos\n2. Ignorer",
		},
		"reg_bio_birth_date": {
			"en": "Date of birth YYYY-MM-DD (optional, reply 0 to skip):",
			"sw": "Tarehe ya kuzaliwa YYYY-MM-DD (hiari, jibu 0 kuruka):",
			"fr": "Date de naissance YYYY-MM-DD (facultatif, répondez 0 pour ignorer):",
		},
		"reg_bio_address": {
			"en": "Street address (optional, reply 0 to skip):",
			"sw": "Anwani ya mtaa (hiari, jibu 0 kuruka):",
			"fr": "Adresse (facultatif, répondez 0 pour ignorer):",
		},
		"reg_bio_city": {
			"en": "City/town (optional, reply 0 to skip):",
			"sw": "Jiji/mji (hiari, jibu 0 kuruka):",
			"fr": "Ville (facultatif, répondez 0 pour ignorer):",
		},
		"reg_bio_postal_code": {
			"en": "Postal code (optional, reply 0 to skip):",
			"sw": "Msimbo wa posta (hiari, jibu 0 kuruka):",
			"fr": "Code postal (facultatif, répondez 0 pour ignorer):",
		},
		"reg_bio_invalid_date": {
			"en": "Invalid date. Enter date of birth as YYYY-MM-DD (or 0 to skip):",
			"sw": "Tarehe batili. Weka tarehe ya kuzaliwa kama YYYY-MM-DD (au 0 kuruka):",
			"fr": "Date invalide. Entrez la date de naissance YYYY-MM-DD (ou 0 pour ignorer):",
		},

		// ── Loan Flow ───────────────────────────────────────────────
		"loan_amount_prompt": {
			"en": "Enter amount to borrow in %s (min %.0f, max %.0f):",
			"sw": "Weka kiasi cha kukopa kwa %s (chini %.0f, juu %.0f):",
			"fr": "Entrez le montant à emprunter en %s (min %.0f, max %.0f):",
		},
		"loan_min_amount": {
			"en": "Minimum loan amount is %s %.0f",
			"sw": "Kiasi cha chini cha mkopo ni %s %.0f",
			"fr": "Le montant minimum du prêt est %s %.0f",
		},
		"loan_max_amount": {
			"en": "The amount requested exceeds the auto-approved limit of %s %.0f",
			"sw": "Kiasi ulichoomba kinazidi kikomo kilichoidhinishwa cha %s %.0f",
			"fr": "Le montant demandé dépasse la limite approuvée de %s %.0f",
		},
		"loan_confirm_summary": {
			"en": "Loan of %s %.0f for %d days\n1. Confirm\n0. Cancel",
			"sw": "Mkopo wa %s %.0f kwa siku %d\n1. Thibitisha\n0. Ghairi",
			"fr": "Prêt de %s %.0f pour %d jours\n1. Confirmer\n0. Annuler",
		},
		"loan_processing": {
			"en": "Your loan of %s %.0f is being processed. You will receive a notification when disbursement is successful.",
			"sw": "Mkopo wako wa %s %.0f unashughulikiwa. Utapokea arifa mara utakapotolewa.",
			"fr": "Votre prêt de %s %.0f est en cours de traitement. Vous recevrez une notification une fois le décaissement effectué.",
		},
		"my_loans_header": {
			"en": "Your Loans:",
			"sw": "Mikopo Yako:",
			"fr": "Vos Prêts:",
		},
		"repay_header": {
			"en": "Repay via M-Pesa:\nPayBill: 123456\nAccount: Your Loan Number\n\nActive Loans:",
			"sw": "Lipa kupitia M-Pesa:\nPayBill: 123456\nAkaunti: Nambari ya Mkopo Wako\n\nMikopo Inayoendelea:",
			"fr": "Remboursez via M-Pesa:\nPayBill: 123456\nCompte: Votre Numéro de Prêt\n\nPrêts Actifs:",
		},
		"repay_loan_line": {
			"en": "Loan: %s\nDue: %s",
			"sw": "Mkopo: %s\nInayodaiwa: %s",
			"fr": "Prêt: %s\nDû: %s",
		},

		// ── Loan Status ─────────────────────────────────────────────
		"loan_status_pending":   {"en": "Pending", "sw": "Inasubiri", "fr": "En attente"},
		"loan_status_approved":  {"en": "Approved", "sw": "Imeidhinishwa", "fr": "Approuvé"},
		"loan_status_disbursed": {"en": "Disbursed", "sw": "Imetolewa", "fr": "Décaissé"},
		"loan_status_repaid":    {"en": "Repaid", "sw": "Imelipwa", "fr": "Remboursé"},
		"loan_status_defaulted": {"en": "Defaulted", "sw": "Imeshindwa kulipwa", "fr": "En défaut"},
		"loan_status_unknown":   {"en": "Unknown", "sw": "Haijulikani", "fr": "Inconnu"},

		// ── Security Question Text (by ID) ──────────────────────────
		"sq_1": {
			"en": "Mother's maiden name?",
			"sw": "Jina la ukoo la mama?",
			"fr": "Nom de jeune fille de votre mère?",
		},
		"sq_2": {
			"en": "Name of your first pet?",
			"sw": "Jina la mnyama wako wa kwanza?",
			"fr": "Nom de votre premier animal?",
		},
		"sq_3": {
			"en": "Town you were born in?",
			"sw": "Mji uliozaliwa?",
			"fr": "Ville de naissance?",
		},
		"sq_4": {
			"en": "Your favorite food?",
			"sw": "Chakula unachopenda?",
			"fr": "Votre plat préféré?",
		},
		"sq_5": {
			"en": "Name of your primary school?",
			"sw": "Jina la shule yako ya msingi?",
			"fr": "Nom de votre école primaire?",
		},
	}

	for key, langs := range translations {
		for lang, msg := range langs {
			l.AddTranslation(key, lang, msg)
		}
	}
}

// NewTranslationBuilder creates a new translation builder
func NewTranslationBuilder(localizer *InMemoryLocalizer) *TranslationBuilder {
	return &TranslationBuilder{
		localizer: localizer,
	}
}

// Add adds a translation with multiple languages
func (tb *TranslationBuilder) Add(key string, translations map[string]string) *TranslationBuilder {
	for lang, msg := range translations {
		tb.localizer.AddTranslation(key, lang, msg)
	}
	return tb
}

// Build returns the localizer
func (tb *TranslationBuilder) Build() *InMemoryLocalizer {
	return tb.localizer
}

// GetLocalizedMessage is a helper function for backward compatibility
func GetLocalizedMessage(language, key string) string {
	// Create a default localizer
	localizer := NewInMemoryLocalizer("en")
	localizer.LoadStandardTranslations()
	return localizer.Get(language, key)
}

// Format provides string formatting with localization
func Format(language, key string, args ...any) string {
	message := GetLocalizedMessage(language, key)
	return fmt.Sprintf(message, args...)
}
