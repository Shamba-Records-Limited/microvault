package ussd

// NewStandardLoanMenuPreset creates a new instance of StandardLoanMenuPreset.
func NewStandardLoanMenuPreset() *StandardLoanMenuPreset {
	return &StandardLoanMenuPreset{}
}

// GetName returns the name of the menu preset.
func (p *StandardLoanMenuPreset) GetName() string {
	return "standard_loan"
}

// Initialize initializes the menu preset.
func (p *StandardLoanMenuPreset) Initialize(registry *MenuRegistry) {
	// Main menu
	mainMenu := NewMenuBuilder("main").
		WithTitle("en", "Welcome to Shamba Records").
		WithTitle("sw", "Karibu Shamba Records").
		WithTitle("fr", "Bienvenue à Shamba Records").
		WithOption("1", map[string]string{
			"en": "Check Balance",
			"sw": "Angalia Salio",
			"fr": "Vérifier Solde",
		}, "check_balance").
		WithOption("2", map[string]string{
			"en": "Request Loan",
			"sw": "Omba Mkopo",
			"fr": "Demander Prêt",
		}, "request_loan").
		WithOption("3", map[string]string{
			"en": "My Loans",
			"sw": "Mikopo Yangu",
			"fr": "Mes Prêts",
		}, "my_loans").
		WithOption("4", map[string]string{
			"en": "Repay Loan",
			"sw": "Lipa Mkopo",
			"fr": "Rembourser Prêt",
		}, "repay_loan").
		WithOption("5", map[string]string{
			"en": "My Account",
			"sw": "Akaunti Yangu",
			"fr": "Mon Compte",
		}, "my_account").
		WithOption("9", map[string]string{
			"en": "Change Language",
			"sw": "Badilisha Lugha",
			"fr": "Changer Langue",
		}, "language_select").
		WithAuth(true).
		Build()
	registry.Register(mainMenu)

	// Language selection
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

	// Loan amount
	loanAmountMenu := NewMenuBuilder("loan_amount").
		WithTitle("en", "Enter loan amount (min 10, max 10000):").
		WithTitle("sw", "Weka kiasi cha mkopo (chini 10, juu 10000):").
		WithTitle("fr", "Entrez montant du prêt (min 10, max 10000):").
		WithAuth(true).
		Build()
	registry.Register(loanAmountMenu)

	// Loan duration
	loanDurationMenu := NewMenuBuilder("loan_duration").
		WithTitle("en", "Select loan duration:").
		WithTitle("sw", "Chagua muda wa mkopo:").
		WithTitle("fr", "Sélectionnez durée du prêt:").
		WithOption("1", map[string]string{
			"en": "7 days",
			"sw": "Siku 7",
			"fr": "7 jours",
		}, "").
		WithOption("2", map[string]string{
			"en": "14 days",
			"sw": "Siku 14",
			"fr": "14 jours",
		}, "").
		WithOption("3", map[string]string{
			"en": "30 days",
			"sw": "Siku 30",
			"fr": "30 jours",
		}, "").
		WithOption("4", map[string]string{
			"en": "90 days",
			"sw": "Siku 90",
			"fr": "90 jours",
		}, "").
		WithOption("0", map[string]string{
			"en": "Back",
			"sw": "Rudi",
			"fr": "Retour",
		}, "main").
		WithAuth(true).
		Build()
	registry.Register(loanDurationMenu)

	// Repayment schedule
	repaymentScheduleMenu := NewMenuBuilder("repayment_schedule").
		WithTitle("en", "Select repayment schedule:").
		WithTitle("sw", "Chagua ratiba ya malipo:").
		WithTitle("fr", "Sélectionnez calendrier de remboursement:").
		WithOption("1", map[string]string{
			"en": "Lump sum (pay at end)",
			"sw": "Jumla moja (lipa mwisho)",
			"fr": "Somme globale (payer à la fin)",
		}, "").
		WithOption("2", map[string]string{
			"en": "Weekly installments",
			"sw": "Malipo ya kila wiki",
			"fr": "Versements hebdomadaires",
		}, "").
		WithOption("3", map[string]string{
			"en": "Bi-weekly installments",
			"sw": "Malipo ya wiki mbili",
			"fr": "Versements bihebdomadaires",
		}, "").
		WithOption("4", map[string]string{
			"en": "Monthly installments",
			"sw": "Malipo ya kila mwezi",
			"fr": "Versements mensuels",
		}, "").
		WithOption("0", map[string]string{
			"en": "Back",
			"sw": "Rudi",
			"fr": "Retour",
		}, "loan_duration").
		WithAuth(true).
		Build()
	registry.Register(repaymentScheduleMenu)

	// My account
	accountMenu := NewMenuBuilder("my_account").
		WithTitle("en", "My Account").
		WithTitle("sw", "Akaunti Yangu").
		WithTitle("fr", "Mon Compte").
		WithOption("1", map[string]string{
			"en": "View Profile",
			"sw": "Angalia Wasifu",
			"fr": "Voir Profil",
		}, "view_profile").
		WithOption("2", map[string]string{
			"en": "Credit Score",
			"sw": "Alama ya Mkopo",
			"fr": "Score de Crédit",
		}, "credit_score").
		WithOption("3", map[string]string{
			"en": "Transaction History",
			"sw": "Historia ya Miamala",
			"fr": "Historique des Transactions",
		}, "transaction_history").
		WithOption("0", map[string]string{
			"en": "Main Menu",
			"sw": "Menyu Kuu",
			"fr": "Menu Principal",
		}, "main").
		WithAuth(true).
		Build()
	registry.Register(accountMenu)
}

// Initialize initializes the menu preset.
func NewSimplifiedMenuPreset() *SimplifiedMenuPreset {
	return &SimplifiedMenuPreset{}
}

// GetName returns the name of the simplified menu preset.
func (p *SimplifiedMenuPreset) GetName() string {
	return "simplified"
}

// Initialize initializes the simplified menu preset.
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

// Initialize initializes the custom menu preset.
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

// Initialize initializes the custom menu preset.
func (p *CustomMenuPreset) Initialize(registry *MenuRegistry) {
	for _, menu := range p.menus {
		registry.Register(menu)
	}
}
