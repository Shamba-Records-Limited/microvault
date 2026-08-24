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
			"en": "Welcome to Microvault",
			"sw": "Karibu Microvault",
			"fr": "Bienvenue à Microvault",
		},
		"goodbye": {
			"en": "Thank you for using Microvault",
			"sw": "Asante kwa kutumia Microvault",
			"fr": "Merci d'utiliser Microvault",
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
		"nav_hint_both": {
			"en": "0 Back  00 Home",
			"sw": "0 Rudi  00 Mwanzo",
			"fr": "0 Retour  00 Accueil",
		},
		"nav_hint_back": {
			"en": "0 Back",
			"sw": "0 Rudi",
			"fr": "0 Retour",
		},
		"nav_hint_home": {
			"en": "00 Home",
			"sw": "00 Mwanzo",
			"fr": "00 Accueil",
		},
		"session_expired": {
			"en": "Your session has expired. Please dial again.",
			"sw": "Kipindi chako kimeisha. Tafadhali piga tena.",
			"fr": "Votre session a expiré. Veuillez composer à nouveau.",
		},
		"registration_success": {
			"en": "Registration successful! You can now request loans.",
			"sw": "Usajili umefanikiwa! Sasa unaweza kuomba mikopo.",
			"fr": "Inscription réussie! Vous pouvez maintenant demander des prets.",
		},
		"loan_request_submitted": {
			"en": "Loan request submitted successfully. You will receive an SMS confirmation.",
			"sw": "Ombi la mkopo limewasilishwa. Utapokea ujumbe wa SMS.",
			"fr": "Demande de pret soumise avec succès. Vous recevrez un SMS de confirmation.",
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
			"fr": "Vous n'avez pas de prets actifs à rembourser",
		},
		"no_loans": {
			"en": "You have no loans",
			"sw": "Huna mikopo",
			"fr": "Vous n'avez pas de prets",
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
		"recovery_success_add_sq": {
			"en": "PIN reset. Add security questions (My Account > PIN Manager) to protect your account and recover it if you lose this phone.",
			"sw": "PIN imewekwa upya. Weka maswali ya usalama (Akaunti Yangu > Dhibiti PIN) kulinda akaunti yako na kuirejesha ukipoteza simu hii.",
			"fr": "PIN réinitialisé. Ajoutez des questions de sécurité (Mon Compte > Gérer PIN) pour protéger et récupérer votre compte.",
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
		"recover_offer": {
			"en": "This ID is already registered. Is this your account?\n1. Recover it on this phone\n2. Re-enter ID",
			"sw": "Kitambulisho hiki kimeshasajiliwa. Je, ni akaunti yako?\n1. Irejeshe kwenye simu hii\n2. Weka tena",
			"fr": "Cette pièce est déjà enregistrée. Est-ce votre compte?\n1. Récupérer sur ce téléphone\n2. Ressaisir",
		},
		"recover_contact_support": {
			"en": "This account has no security questions, so it cannot be moved to a new phone here. Please contact support.",
			"sw": "Akaunti hii haina maswali ya usalama, haiwezi kuhamishwa hapa. Tafadhali wasiliana na msaada.",
			"fr": "Ce compte n'a pas de questions de sécurité, il ne peut pas etre transféré ici. Contactez le support.",
		},
		"recover_success": {
			"en": "Account recovered to this phone. Dial again and sign in with your existing PIN.",
			"sw": "Akaunti imerejeshwa kwenye simu hii. Piga tena na uingie na PIN yako ya sasa.",
			"fr": "Compte récupéré sur ce téléphone. Composez à nouveau et connectez-vous avec votre PIN.",
		},
		"reg_national_id_taken": {
			"en": "This national ID is already registered. Re-enter, or contact support if this is you:",
			"sw": "Kitambulisho hiki kimeshasajiliwa. Weka tena, au wasiliana na msaada kama ni chako:",
			"fr": "Cette pièce d'identité est déjà enregistrée. Ressaisissez, ou contactez le support si c'est vous:",
		},
		// My Details — the picker lists each field with its stored value so a
		// user can see what is on file before choosing one to change.
		"my_details_title": {
			"en": "My Details",
			"sw": "Maelezo Yangu",
			"fr": "Mes Infos",
		},
		"my_details_not_set": {
			"en": "not set",
			"sw": "haijawekwa",
			"fr": "non defini",
		},
		"my_details_back": {
			"en": "0. Back",
			"sw": "0. Rudi",
			"fr": "0. Retour",
		},
		"label_birth_date": {
			"en": "DOB",
			"sw": "Kuzaliwa",
			"fr": "Naissance",
		},
		"label_address": {
			"en": "Address",
			"sw": "Anwani",
			"fr": "Adresse",
		},
		"label_city": {
			"en": "City",
			"sw": "Jiji",
			"fr": "Ville",
		},
		"label_postal_code": {
			"en": "Postal",
			"sw": "Posta",
			"fr": "Postal",
		},
		"bio_field_saved": {
			"en": "Saved.",
			"sw": "Imehifadhiwa.",
			"fr": "Enregistre.",
		},
		"bio_prompt_birth_date": {
			"en": "Date of birth YYYY-MM-DD (0 to cancel):",
			"sw": "Tarehe ya kuzaliwa YYYY-MM-DD (0 kughairi):",
			"fr": "Date de naissance YYYY-MM-DD (0 pour annuler):",
		},
		"bio_prompt_address": {
			"en": "Street address (0 to cancel):",
			"sw": "Anwani ya mtaa (0 kughairi):",
			"fr": "Adresse (0 pour annuler):",
		},
		"bio_prompt_city": {
			"en": "City/town (0 to cancel):",
			"sw": "Jiji/mji (0 kughairi):",
			"fr": "Ville (0 pour annuler):",
		},
		"bio_prompt_postal_code": {
			"en": "Postal code (0 to cancel):",
			"sw": "Msimbo wa posta (0 kughairi):",
			"fr": "Code postal (0 pour annuler):",
		},
		"bio_invalid_date": {
			"en": "Invalid date. Enter as YYYY-MM-DD (0 to cancel):",
			"sw": "Tarehe batili. Weka kama YYYY-MM-DD (0 kughairi):",
			"fr": "Date invalide. Entrez YYYY-MM-DD (0 pour annuler):",
		},
		"badge_no_security_q": {
			"en": "Add security questions to protect your account.",
			"sw": "Weka maswali ya usalama kulinda akaunti yako.",
			"fr": "Ajoutez des questions de sécurité pour protéger votre compte.",
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
			"fr": "Le montant minimum du pret est %s %.0f",
		},
		"loan_max_amount": {
			"en": "The amount requested exceeds the auto-approved limit of %s %.0f",
			"sw": "Kiasi ulichoomba kinazidi kikomo kilichoidhinishwa cha %s %.0f",
			"fr": "Le montant demandé dépasse la limite approuvée de %s %.0f",
		},
		"loan_cash_pickup_min": {
			"en": "Cash pickup needs at least %s %.0f. Choose mobile money instead:",
			"sw": "Kuchukua pesa kunahitaji angalau %s %.0f. Chagua pesa ya simu badala yake:",
			"fr": "Le retrait en espèces exige au moins %s %.0f. Choisissez plutot mobile money:",
		},
		// The terms and the PIN gate share one screen: entering the PIN is the
		// act of accepting what is displayed above it. The back and home hints
		// are appended by withNavHint rather than spelled out here.
		"loan_confirm_summary": {
			"en": "Loan of %s %.0f for %d days\nEnter PIN to confirm:",
			"sw": "Mkopo wa %s %.0f kwa siku %d\nWeka PIN kuthibitisha:",
			"fr": "Pret de %s %.0f pour %d jours\nEntrez PIN pour confirmer:",
		},
		"loan_confirm_pin_format": {
			"en": "Enter your 4-digit PIN.",
			"sw": "Weka PIN yako ya tarakimu 4.",
			"fr": "Entrez votre PIN à 4 chiffres.",
		},
		"loan_processing": {
			"en": "Your loan of %s %.0f is being processed. You will receive a notification when disbursement is successful.",
			"sw": "Mkopo wako wa %s %.0f unashughulikiwa. Utapokea arifa mara utakapotolewa.",
			"fr": "Votre pret de %s %.0f est en cours de traitement. Vous recevrez une notification une fois le décaissement effectué.",
		},
		"my_loans_processing": {
			"en": "Your loan is still being processed. We will text you when it is ready.",
			"sw": "Mkopo wako bado unashughulikiwa. Tutakutumia SMS ukiwa tayari.",
			"fr": "Votre pret est en cours de traitement. Nous vous enverrons un SMS.",
		},
		"my_loans_sent": {
			"en": "We have sent your loan details by SMS.",
			"sw": "Tumetuma maelezo ya mkopo wako kwa SMS.",
			"fr": "Nous avons envoye les details de votre pret par SMS.",
		},
		"repay_header": {
			"en": "Repay a loan. Reply with a number:",
			"sw": "Lipa mkopo. Jibu na nambari:",
			"fr": "Rembourser un prêt. Répondez par un numéro:",
		},
		"repay_loan_line": {
			"en": "%s - owe %s",
			"sw": "%s - deni %s",
			"fr": "%s - doit %s",
		},
		"repay_rail_header": {
			"en": "Loan %s, owe %s.\nHow will you pay?",
			"sw": "Mkopo %s, deni %s.\nUtalipaje?",
			"fr": "Prêt %s, doit %s.\nComment payerez-vous?",
		},
		"repay_rail_cash": {
			"en": "1. Cash at MoneyGram",
			"sw": "1. Fedha taslimu MoneyGram",
			"fr": "1. Especes a MoneyGram",
		},
		"repay_rail_mobile": {
			"en": "2. Mobile money",
			"sw": "2. Pesa ya simu",
			"fr": "2. Argent mobile",
		},
		"repay_cash_sent": {
			"en": "We sent you an SMS with a link to finish paying %s at a MoneyGram agent.",
			"sw": "Tumekutumia SMS yenye kiungo cha kumaliza kulipa %s kwa wakala wa MoneyGram.",
			"fr": "Nous vous avons envoye un SMS avec un lien pour payer %s chez un agent MoneyGram.",
		},
		"repay_paybill": {
			"en": "Pay to PayBill %s.\nAccount: %s",
			"sw": "Lipa kwa PayBill %s.\nAkaunti: %s",
			"fr": "Payez au PayBill %s.\nCompte: %s",
		},
		"repay_no_rail": {
			"en": "No repayment method is available right now. Please try again later.",
			"sw": "Hakuna njia ya kulipa kwa sasa. Tafadhali jaribu tena baadaye.",
			"fr": "Aucun moyen de remboursement disponible. Reessayez plus tard.",
		},

		// ── Loan Status ─────────────────────────────────────────────

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
