package merkle

type screenRequest struct {
	Identifier string `json:"identifier"`
	Blockchain string `json:"blockchain"`
	CustomerID string `json:"customer_id,omitempty"`
	Address    string `json:"address,omitempty"`
	Type       int    `json:"type"`
	ShowAlerts bool   `json:"show_alerts"`
}

type tag struct {
	TagType    string  `json:"tag_type_verbose"`
	TagSubtype string  `json:"tag_subtype_verbose"`
	TagName    string  `json:"tag_name_verbose"`
	ValueUSD   float64 `json:"total_value_usd"`
	Exposure   string  `json:"exposure_type"`
}

type ownTags struct {
	Owner *tag `json:"owner"`
	User  *tag `json:"user"`
}

type alert struct {
	ID            string `json:"id"`
	Level         int    `json:"level"`
	RuleTriggered string `json:"rule_triggered"`
	Policy        string `json:"policy_category_verbose"`
}

// screenResponse is the part of an address or transaction assessment the
// verdict reads; the whole body is kept as the screening's raw payload.
type screenResponse struct {
	Identifier       string   `json:"identifier"`
	RiskLevel        *int     `json:"risk_level"`
	RiskLevelVerbose string   `json:"risk_level_verbose"`
	EarliestTxTime   *string  `json:"earliest_transaction_time"`
	LatestTxTime     *string  `json:"latest_transaction_time"`
	Tags             *ownTags `json:"tags"`
	Originator       []tag    `json:"originator"`
	Beneficiary      []tag    `json:"beneficiary"`
	Alerts           []alert  `json:"alerts"`
}
