package notifications

// LoanOption configures an [SMSLoanNotifier] at construction.
type LoanOption func(*loanConfig)

// AccountOption configures an [SMSAccountNotifier] at construction.
type AccountOption func(*accountConfig)

type loanConfig struct {
	overrides map[string]*LoanTemplates
	resolve   LanguageResolver
}

type accountConfig struct {
	overrides map[string]*AccountTemplates
	resolve   LanguageResolver
}

// WithLoanTemplates overrides loan copy for one language. Fields left nil keep
// the platform default, so a builder writes only the messages it wants to
// change. Calling it more than once for the same language replaces the earlier
// override rather than merging the two.
func WithLoanTemplates(lang string, t *LoanTemplates) LoanOption {
	return func(c *loanConfig) {
		if c.overrides == nil {
			c.overrides = map[string]*LoanTemplates{}
		}
		c.overrides[lang] = t
	}
}

// WithLoanTemplateSet applies [WithLoanTemplates] for every language in the
// map, which is how a builder that keeps its copy in one place hands the whole
// set over at once.
func WithLoanTemplateSet(set map[string]*LoanTemplates) LoanOption {
	return func(c *loanConfig) {
		for lang, t := range set {
			WithLoanTemplates(lang, t)(c)
		}
	}
}

// WithLoanLanguageResolver supplies the resolver consulted when a notification
// leaves Language empty. Without one, such notifications render in English.
func WithLoanLanguageResolver(r LanguageResolver) LoanOption {
	return func(c *loanConfig) { c.resolve = r }
}

// WithAccountTemplates overrides account and PIN copy for one language, with
// the same per-field fallback as [WithLoanTemplates].
func WithAccountTemplates(lang string, t *AccountTemplates) AccountOption {
	return func(c *accountConfig) {
		if c.overrides == nil {
			c.overrides = map[string]*AccountTemplates{}
		}
		c.overrides[lang] = t
	}
}

// WithAccountTemplateSet applies [WithAccountTemplates] for every language in
// the map.
func WithAccountTemplateSet(set map[string]*AccountTemplates) AccountOption {
	return func(c *accountConfig) {
		for lang, t := range set {
			WithAccountTemplates(lang, t)(c)
		}
	}
}

// WithAccountLanguageResolver supplies the resolver consulted when a
// notification leaves Language empty.
func WithAccountLanguageResolver(r LanguageResolver) AccountOption {
	return func(c *accountConfig) { c.resolve = r }
}
