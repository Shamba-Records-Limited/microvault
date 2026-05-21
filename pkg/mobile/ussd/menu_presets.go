// Package ussd provides predefined menu flows, layouts, and registration sequences.
package ussd

// NewStandardLoanMenuPreset creates a new instance of StandardLoanMenuPreset.
func NewStandardLoanMenuPreset() *StandardLoanMenuPreset {
	return &StandardLoanMenuPreset{}
}

// GetName returns the name of the menu preset.
func (p *StandardLoanMenuPreset) GetName() string {
	return "standard_loan"
}

// Initialize registers all menus for the standard loan flow including the
// restructured main menu (4 items), PIN management menus, and security
// question setup menus.
func (p *StandardLoanMenuPreset) Initialize(registry *MenuRegistry) {
	// Main Menu (4 options)
	mainMenu := NewMenuBuilder("main").
		WithTitle("en", "Welcome to Shamba Records").
		WithTitle("sw", "Karibu Shamba Records").
		WithTitle("fr", "Bienvenue à Shamba Records").
		WithOption("1", map[string]string{
			"en": "Request Loan",
			"sw": "Omba Mkopo",
			"fr": "Demander Prêt",
		}, "request_loan").
		WithOption("2", map[string]string{
			"en": "Repay Loan",
			"sw": "Lipa Mkopo",
			"fr": "Rembourser Prêt",
		}, "repay_loan").
		WithOption("3", map[string]string{
			"en": "My Loans",
			"sw": "Mikopo Yangu",
			"fr": "Mes Prêts",
		}, "my_loans").
		WithOption("4", map[string]string{
			"en": "My Account",
			"sw": "Akaunti Yangu",
			"fr": "Mon Compte",
		}, "my_account").
		WithAuth(true).
		Build()
	registry.Register(mainMenu)

	// Language Selection
	langMenu := NewMenuBuilder("language_select").
		WithTitle("en", "Select Language / Chagua Lugha / Choisir Langue").
		WithOption("1", map[string]string{"en": "English"}, "main").
		WithOption("2", map[string]string{"en": "Kiswahili"}, "main").
		WithOption("3", map[string]string{"en": "Français"}, "main").
		WithOption("0", map[string]string{"en": "Back / Rudi / Retour"}, "main").
		WithAuth(false).
		Build()
	registry.Register(langMenu)

	// Registration
	registerMenu := NewMenuBuilder("register").
		WithTitle("en", "Register for Shamba Records\nEnter your full name:").
		WithTitle("sw", "Jisajili kwa Shamba Records\nWeka jina lako kamili:").
		WithTitle("fr", "S'inscrire à Shamba Records\nEntrez votre nom complet:").
		WithAuth(false).
		Build()
	registry.Register(registerMenu)

	// Loan Flow
	// The loan_amount prompt is rendered dynamically by showLoanAmountMenu
	// using the active loan product's fiat limits. This static registration
	// serves as a fallback only.
	loanAmountMenu := NewMenuBuilder("loan_amount").
		WithTitle("en", "Enter loan amount:").
		WithTitle("sw", "Weka kiasi cha mkopo:").
		WithTitle("fr", "Entrez montant du prêt:").
		WithAuth(true).
		Build()
	registry.Register(loanAmountMenu)

	// Payout method picker — shown after the user enters an amount. Mobile
	// Money routes to YellowCard; Cash Pickup routes to MoneyGram and
	// branches into the SEP-9 birth-date prompt.
	payoutMethodMenu := NewMenuBuilder("payout_method").
		WithTitle("en", "How would you like to receive your loan?").
		WithTitle("sw", "Ungependa kupokea mkopo wako vipi?").
		WithTitle("fr", "Comment souhaitez-vous recevoir votre prêt?").
		WithOption("1", map[string]string{
			"en": "Cash Pickup (MoneyGram)",
			"sw": "Kuchukua Pesa (MoneyGram)",
			"fr": "Retrait en Espèces (MoneyGram)",
		}, "loan_birthdate").
		WithOption("2", map[string]string{
			"en": "Mobile Money",
			"sw": "Pesa za Simu",
			"fr": "Mobile Money",
		}, "loan_confirm").
		WithAuth(true).
		Build()
	registry.Register(payoutMethodMenu)

	// Birth-date prompt — required for the MoneyGram SEP-9 customer prefill
	// so the user doesn't have to retype it inside the webview.
	loanBirthdateMenu := NewMenuBuilder("loan_birthdate").
		WithTitle("en", "Enter your date of birth (YYYY-MM-DD):").
		WithTitle("sw", "Weka tarehe yako ya kuzaliwa (YYYY-MM-DD):").
		WithTitle("fr", "Entrez votre date de naissance (AAAA-MM-JJ):").
		WithAuth(true).
		Build()
	registry.Register(loanBirthdateMenu)

	// My Account (PIN Manager + Change Language)
	accountMenu := NewMenuBuilder("my_account").
		WithTitle("en", "My Account").
		WithTitle("sw", "Akaunti Yangu").
		WithTitle("fr", "Mon Compte").
		WithOption("1", map[string]string{
			"en": "PIN Manager",
			"sw": "Dhibiti PIN",
			"fr": "Gérer PIN",
		}, "pin_manager").
		WithOption("2", map[string]string{
			"en": "Change Language",
			"sw": "Badilisha Lugha",
			"fr": "Changer Langue",
		}, "language_select").
		WithOption("0", map[string]string{
			"en": "Main Menu",
			"sw": "Menyu Kuu",
			"fr": "Menu Principal",
		}, "main").
		WithAuth(true).
		Build()
	registry.Register(accountMenu)

	// PIN Manager
	pinManagerMenu := NewMenuBuilder("pin_manager").
		WithTitle("en", "PIN Manager").
		WithTitle("sw", "Dhibiti PIN").
		WithTitle("fr", "Gérer PIN").
		WithOption("1", map[string]string{
			"en": "Change PIN",
			"sw": "Badilisha PIN",
			"fr": "Changer PIN",
		}, "pin_change_old").
		WithOption("2", map[string]string{
			"en": "Security Questions",
			"sw": "Maswali ya Usalama",
			"fr": "Questions de Sécurité",
		}, "security_q1_select").
		WithOption("0", map[string]string{
			"en": "Back", "sw": "Rudi", "fr": "Retour",
		}, "my_account").
		WithAuth(true).
		Build()
	registry.Register(pinManagerMenu)

	// PIN Creation (during registration)
	pinCreateMenu := NewMenuBuilder("pin_create").
		WithTitle("en", "Create a 4-digit PIN:").
		WithTitle("sw", "Tengeneza PIN ya tarakimu 4:").
		WithTitle("fr", "Créez un PIN à 4 chiffres:").
		WithAuth(false).
		Build()
	registry.Register(pinCreateMenu)

	pinConfirmMenu := NewMenuBuilder("pin_confirm").
		WithTitle("en", "Confirm your PIN:").
		WithTitle("sw", "Thibitisha PIN yako:").
		WithTitle("fr", "Confirmez votre PIN:").
		WithAuth(false).
		Build()
	registry.Register(pinConfirmMenu)

	// PIN Change
	pinChangeOldMenu := NewMenuBuilder("pin_change_old").
		WithTitle("en", "Enter current PIN:").
		WithTitle("sw", "Weka PIN ya sasa:").
		WithTitle("fr", "Entrez PIN actuel:").
		WithAuth(true).
		Build()
	registry.Register(pinChangeOldMenu)

	pinChangeNewMenu := NewMenuBuilder("pin_change_new").
		WithTitle("en", "Enter new 4-digit PIN:").
		WithTitle("sw", "Weka PIN mpya ya tarakimu 4:").
		WithTitle("fr", "Entrez nouveau PIN à 4 chiffres:").
		WithAuth(true).
		Build()
	registry.Register(pinChangeNewMenu)

	pinChangeConfirmMenu := NewMenuBuilder("pin_change_confirm").
		WithTitle("en", "Confirm new PIN:").
		WithTitle("sw", "Thibitisha PIN mpya:").
		WithTitle("fr", "Confirmez nouveau PIN:").
		WithAuth(true).
		Build()
	registry.Register(pinChangeConfirmMenu)

	// PIN Verification Gates
	pinVerifyLoanMenu := NewMenuBuilder("pin_verify_loan").
		WithTitle("en", "Enter your 4-digit PIN to confirm loan:").
		WithTitle("sw", "Weka PIN yako ya tarakimu 4 kuthibitisha mkopo:").
		WithTitle("fr", "Entrez votre PIN à 4 chiffres pour confirmer le prêt:").
		WithAuth(true).
		Build()
	registry.Register(pinVerifyLoanMenu)

	pinVerifyRepayMenu := NewMenuBuilder("pin_verify_repay").
		WithTitle("en", "Enter your 4-digit PIN:").
		WithTitle("sw", "Weka PIN yako ya tarakimu 4:").
		WithTitle("fr", "Entrez votre PIN à 4 chiffres:").
		WithAuth(true).
		Build()
	registry.Register(pinVerifyRepayMenu)

	// Security Questions Setup
	secQ1SelectMenu := NewMenuBuilder("security_q1_select").
		WithTitle("en", "Select security question 1:").
		WithTitle("sw", "Chagua swali la usalama la 1:").
		WithTitle("fr", "Choisissez question de sécurité 1:").
		WithOption("1", map[string]string{
			"en": "Mother's maiden name?",
			"sw": "Jina la ukoo la mama?",
			"fr": "Nom de jeune fille de votre mère?",
		}, "").
		WithOption("2", map[string]string{
			"en": "Name of your first pet?",
			"sw": "Jina la mnyama wako wa kwanza?",
			"fr": "Nom de votre premier animal?",
		}, "").
		WithOption("3", map[string]string{
			"en": "Town you were born in?",
			"sw": "Mji uliozaliwa?",
			"fr": "Ville de naissance?",
		}, "").
		WithOption("4", map[string]string{
			"en": "Your favorite food?",
			"sw": "Chakula unachopenda?",
			"fr": "Votre plat préféré?",
		}, "").
		WithOption("5", map[string]string{
			"en": "Name of your primary school?",
			"sw": "Jina la shule yako ya msingi?",
			"fr": "Nom de votre école primaire?",
		}, "").
		WithAuth(false).
		Build()
	registry.Register(secQ1SelectMenu)

	secQ1AnswerMenu := NewMenuBuilder("security_q1_answer").
		WithTitle("en", "Enter your answer:").
		WithTitle("sw", "Weka jibu lako:").
		WithTitle("fr", "Entrez votre réponse:").
		WithAuth(false).
		Build()
	registry.Register(secQ1AnswerMenu)

	secQ2SelectMenu := NewMenuBuilder("security_q2_select").
		WithTitle("en", "Select security question 2:").
		WithTitle("sw", "Chagua swali la usalama la 2:").
		WithTitle("fr", "Choisissez question de sécurité 2:").
		// Options are dynamically rendered minus the Q1 selection
		WithOption("1", map[string]string{
			"en": "Mother's maiden name?",
			"sw": "Jina la ukoo la mama?",
			"fr": "Nom de jeune fille de votre mère?",
		}, "").
		WithOption("2", map[string]string{
			"en": "Name of your first pet?",
			"sw": "Jina la mnyama wako wa kwanza?",
			"fr": "Nom de votre premier animal?",
		}, "").
		WithOption("3", map[string]string{
			"en": "Town you were born in?",
			"sw": "Mji uliozaliwa?",
			"fr": "Ville de naissance?",
		}, "").
		WithOption("4", map[string]string{
			"en": "Your favorite food?",
			"sw": "Chakula unachopenda?",
			"fr": "Votre plat préféré?",
		}, "").
		WithOption("5", map[string]string{
			"en": "Name of your primary school?",
			"sw": "Jina la shule yako ya msingi?",
			"fr": "Nom de votre école primaire?",
		}, "").
		WithAuth(false).
		Build()
	registry.Register(secQ2SelectMenu)

	secQ2AnswerMenu := NewMenuBuilder("security_q2_answer").
		WithTitle("en", "Enter your answer:").
		WithTitle("sw", "Weka jibu lako:").
		WithTitle("fr", "Entrez votre réponse:").
		WithAuth(false).
		Build()
	registry.Register(secQ2AnswerMenu)

	// PIN Recovery
	pinRecoveryIDMenu := NewMenuBuilder("pin_recovery_national_id").
		WithTitle("en", "Enter your national ID to verify:").
		WithTitle("sw", "Weka kitambulisho chako kuthibitisha:").
		WithTitle("fr", "Entrez votre pièce d'identité:").
		WithAuth(false).
		Build()
	registry.Register(pinRecoveryIDMenu)

	pinRecoveryQ1Menu := NewMenuBuilder("pin_recovery_q1").
		WithTitle("en", "Answer your security question:").
		WithTitle("sw", "Jibu swali lako la usalama:").
		WithTitle("fr", "Répondez à votre question de sécurité:").
		WithAuth(false).
		Build()
	registry.Register(pinRecoveryQ1Menu)

	pinRecoveryQ2Menu := NewMenuBuilder("pin_recovery_q2").
		WithTitle("en", "Answer your security question:").
		WithTitle("sw", "Jibu swali lako la usalama:").
		WithTitle("fr", "Répondez à votre question de sécurité:").
		WithAuth(false).
		Build()
	registry.Register(pinRecoveryQ2Menu)

	pinRecoveryNewMenu := NewMenuBuilder("pin_recovery_new").
		WithTitle("en", "Enter new 4-digit PIN:").
		WithTitle("sw", "Weka PIN mpya ya tarakimu 4:").
		WithTitle("fr", "Entrez nouveau PIN à 4 chiffres:").
		WithAuth(false).
		Build()
	registry.Register(pinRecoveryNewMenu)

	pinRecoveryConfirmMenu := NewMenuBuilder("pin_recovery_confirm").
		WithTitle("en", "Confirm new PIN:").
		WithTitle("sw", "Thibitisha PIN mpya:").
		WithTitle("fr", "Confirmez nouveau PIN:").
		WithAuth(false).
		Build()
	registry.Register(pinRecoveryConfirmMenu)
}

// NewSimplifiedMenuPreset creates a new instance of SimplifiedMenuPreset.
func NewSimplifiedMenuPreset() *SimplifiedMenuPreset {
	return &SimplifiedMenuPreset{}
}

// GetName returns the name of the simplified menu preset.
func (p *SimplifiedMenuPreset) GetName() string {
	return "simplified"
}

// Initialize registers the simplified menu preset.
func (p *SimplifiedMenuPreset) Initialize(registry *MenuRegistry) {
	// Simplified main menu with fewer options
	mainMenu := NewMenuBuilder("main").
		WithTitle("en", "Shamba Records Quick Menu").
		WithTitle("sw", "Menyu ya Haraka ya Shamba Records").
		WithOption("1", map[string]string{
			"en": "Get Loan",
			"sw": "Pata Mkopo",
		}, "request_loan").
		WithOption("2", map[string]string{
			"en": "Check Loans",
			"sw": "Angalia Mikopo",
		}, "my_loans").
		WithOption("3", map[string]string{
			"en": "Account",
			"sw": "Akaunti",
		}, "my_account").
		WithAuth(true).
		Build()
	registry.Register(mainMenu)

	// Add other simplified menus...
}

// NewCustomMenuPreset creates a new instance of CustomMenuPreset.
func NewCustomMenuPreset(name string) *CustomMenuPreset {
	return &CustomMenuPreset{
		name:  name,
		menus: make([]*Menu, 0),
	}
}

// GetName returns the name of the custom menu preset.
func (p *CustomMenuPreset) GetName() string {
	return p.name
}

// AddMenu adds a menu to the custom menu preset.
func (p *CustomMenuPreset) AddMenu(menu *Menu) *CustomMenuPreset {
	p.menus = append(p.menus, menu)
	return p
}

// NewCustomMenuPreset creates a new instance of CustomMenuPreset.
func (p *CustomMenuPreset) Initialize(registry *MenuRegistry) {
	for _, menu := range p.menus {
		registry.Register(menu)
	}
}
