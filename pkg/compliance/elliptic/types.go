package elliptic

// walletScreeningRequest is the POST /v2/wallet/synchronous body — source
// design doc §4.
type walletScreeningRequest struct {
	Subject           walletSubject `json:"subject"`
	Type              string        `json:"type"`
	CustomerReference string        `json:"customer_reference,omitempty"`
}

type walletSubject struct {
	Asset      string `json:"asset"`
	Blockchain string `json:"blockchain"`
	Type       string `json:"type"`
	Hash       string `json:"hash"`
}

// walletScreeningResponse models the fields the source design doc's §4
// field table names as ones we act on. Elliptic's actual response is
// larger; everything else is preserved verbatim in Screening.Raw rather
// than modeled here, per the doc's own instruction to keep the audit
// record reproducible against fields we haven't parsed.
//
// One field's exact wire name is a documented assumption pending the first
// real sandbox call: the analysis's own top-level ID. The doc's field
// table never names it explicitly (only ScreeningID/ScreeningSource are
// named), so `id` is a best-effort guess at ordinary REST convention. It
// is not load-bearing for the verdict — see verdict.go — so a wrong guess
// here degrades to an empty Screening.AnalysisID, not a wrong decision.
type walletScreeningResponse struct {
	ID               string            `json:"id"`
	RiskScore        *float64          `json:"risk_score"`
	ClusterEntities  []clusterEntity   `json:"cluster_entities,omitempty"`
	EvaluationDetail *evaluationDetail `json:"evaluation_detail,omitempty"`
	ProcessStatus    string            `json:"process_status"`
	WorkflowStatus   string            `json:"workflow_status,omitempty"`
	ScreeningID      string            `json:"screening_id"`
	ScreeningSource  string            `json:"screening_source,omitempty"`
	Error            *errorDetail      `json:"error,omitempty"`
}

type errorDetail struct {
	Message string `json:"message"`
}

type clusterEntity struct {
	IsVASP              bool           `json:"is_vasp"`
	IsPrimaryEntity     bool           `json:"is_primary_entity"`
	IsAfterSanctionDate bool           `json:"is_after_sanction_date"`
	EntityDetails       *entityDetails `json:"entity_details,omitempty"`
}

type entityDetails struct {
	Sanctions []sanctionEntry `json:"sanctions,omitempty"`
}

type sanctionEntry struct {
	List      sanctionList `json:"list"`
	DateAdded string       `json:"date_added"`
	URL       string       `json:"url"`
}

type sanctionList struct {
	Key string `json:"key"`
}

type evaluationDetail struct {
	Source []ruleEvaluation `json:"source,omitempty"`
}

type ruleEvaluation struct {
	RuleName string `json:"rule_name"`
	RuleType string `json:"rule_type"`
}
