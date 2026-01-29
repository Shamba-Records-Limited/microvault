package ussd

import "fmt"

// NetworkMapping maps telco's mobile network code (MCC + MNC) to MoMo network info
type NetworkMapping struct {
	MobileNetworkCode string // Telco's mobile network code (MCC + MNC) (e.g., "63902")
	MomoNetworkCode   string // Momo network code (e.g., "MPESA")
	MomoNetworkName   string // Momo Display name (e.g., "M-Pesa")
	TelcoName         string // Telco name (e.g., "Safaricom Kenya")
	Country           string // ISO country code (e.g., "KE")
}

// NetworkMappings maps telco's mobile network code (MCC + MNC) to mobile money networks
var NetworkMappings = map[string]NetworkMapping{
	// Kenya
	"63902": {MobileNetworkCode: "63902", MomoNetworkCode: "MPESA", MomoNetworkName: "M-Pesa", TelcoName: "Safaricom Kenya", Country: "KE"},
	"63903": {MobileNetworkCode: "63903", MomoNetworkCode: "AIRTEL", MomoNetworkName: "Airtel Money", TelcoName: "Airtel Kenya", Country: "KE"},
	"63907": {MobileNetworkCode: "63907", MomoNetworkCode: "TKASH", MomoNetworkName: "T-Kash", TelcoName: "Orange Kenya", Country: "KE"},
	"63999": {MobileNetworkCode: "63999", MomoNetworkCode: "EQUITEL", MomoNetworkName: "Equitel", TelcoName: "Equitel Kenya", Country: "KE"},

	// Uganda
	"64101": {MobileNetworkCode: "64101", MomoNetworkCode: "AIRTEL_UG", MomoNetworkName: "Airtel Money", TelcoName: "Airtel Uganda", Country: "UG"},
	"64110": {MobileNetworkCode: "64110", MomoNetworkCode: "MTN_UG", MomoNetworkName: "MTN Mobile Money", TelcoName: "MTN Uganda", Country: "UG"},
	"64114": {MobileNetworkCode: "64114", MomoNetworkCode: "AFRICELL_UG", MomoNetworkName: "Africell Money", TelcoName: "Africell Uganda", Country: "UG"},

	// Tanzania
	"64002": {MobileNetworkCode: "64002", MomoNetworkCode: "TIGO_TZ", MomoNetworkName: "Tigo Pesa", TelcoName: "Tigo Tanzania", Country: "TZ"},
	"64004": {MobileNetworkCode: "64004", MomoNetworkCode: "VODACOM_TZ", MomoNetworkName: "M-Pesa", TelcoName: "Vodacom Tanzania", Country: "TZ"},
	"64005": {MobileNetworkCode: "64005", MomoNetworkCode: "AIRTEL_TZ", MomoNetworkName: "Airtel Money", TelcoName: "Airtel Tanzania", Country: "TZ"},

	// Rwanda
	"63510": {MobileNetworkCode: "63510", MomoNetworkCode: "MTN_RW", MomoNetworkName: "MTN Mobile Money", TelcoName: "MTN Rwanda", Country: "RW"},
	"63513": {MobileNetworkCode: "63513", MomoNetworkCode: "TIGO_RW", MomoNetworkName: "Tigo Cash", TelcoName: "Tigo Rwanda", Country: "RW"},
	"63514": {MobileNetworkCode: "63514", MomoNetworkCode: "AIRTEL_RW", MomoNetworkName: "Airtel Money", TelcoName: "Airtel Rwanda", Country: "RW"},

	// Nigeria
	"62120": {MobileNetworkCode: "62120", MomoNetworkCode: "AIRTEL_NG", MomoNetworkName: "Airtel Money", TelcoName: "Airtel Nigeria", Country: "NG"},
	"62130": {MobileNetworkCode: "62130", MomoNetworkCode: "MTN_NG", MomoNetworkName: "MTN MoMo", TelcoName: "MTN Nigeria", Country: "NG"},
	"62150": {MobileNetworkCode: "62150", MomoNetworkCode: "GLO_NG", MomoNetworkName: "Glo Mobile Money", TelcoName: "Glo Nigeria", Country: "NG"},
	"62160": {MobileNetworkCode: "62160", MomoNetworkCode: "ETISALAT_NG", MomoNetworkName: "9Mobile", TelcoName: "Etisalat Nigeria", Country: "NG"},

	// Ghana
	"62001": {MobileNetworkCode: "62001", MomoNetworkCode: "MTN_GH", MomoNetworkName: "MTN Mobile Money", TelcoName: "MTN Ghana", Country: "GH"},
	"62002": {MobileNetworkCode: "62002", MomoNetworkCode: "VODAFONE_GH", MomoNetworkName: "Vodafone Cash", TelcoName: "Vodafone Ghana", Country: "GH"},
	"62006": {MobileNetworkCode: "62006", MomoNetworkCode: "AIRTELTIGO_GH", MomoNetworkName: "AirtelTigo Money", TelcoName: "AirtelTigo Ghana", Country: "GH"},

	// Ethiopia
	"63601": {MobileNetworkCode: "63601", MomoNetworkCode: "ETHIO", MomoNetworkName: "Telebirr", TelcoName: "EthioTelecom", Country: "ET"},

	// Zambia
	"64501": {MobileNetworkCode: "64501", MomoNetworkCode: "AIRTEL_ZM", MomoNetworkName: "Airtel Money", TelcoName: "Airtel Zambia", Country: "ZM"},
	"64502": {MobileNetworkCode: "64502", MomoNetworkCode: "MTN_ZM", MomoNetworkName: "MTN Mobile Money", TelcoName: "MTN Zambia", Country: "ZM"},

	// Malawi
	"65001": {MobileNetworkCode: "65001", MomoNetworkCode: "TNM_MW", MomoNetworkName: "TNM Mpamba", TelcoName: "TNM Malawi", Country: "MW"},
	"65010": {MobileNetworkCode: "65010", MomoNetworkCode: "AIRTEL_MW", MomoNetworkName: "Airtel Money", TelcoName: "Airtel Malawi", Country: "MW"},

	// South Africa
	"65501": {MobileNetworkCode: "65501", MomoNetworkCode: "VODACOM_ZA", MomoNetworkName: "Vodacom", TelcoName: "Vodacom South Africa", Country: "ZA"},
	"65502": {MobileNetworkCode: "65502", MomoNetworkCode: "TELKOM_ZA", MomoNetworkName: "Telkom Pay", TelcoName: "Telkom South Africa", Country: "ZA"},
	"65507": {MobileNetworkCode: "65507", MomoNetworkCode: "CELLC_ZA", MomoNetworkName: "CellC", TelcoName: "CellC South Africa", Country: "ZA"},
	"65510": {MobileNetworkCode: "65510", MomoNetworkCode: "MTN_ZA", MomoNetworkName: "MTN Mobile Money", TelcoName: "MTN South Africa", Country: "ZA"},

	// Sandbox
	"99999": {MobileNetworkCode: "99999", MomoNetworkCode: "SANDBOX", MomoNetworkName: "Sandbox Network", TelcoName: "Athena", Country: "KE"},
}

// NetworkMapper maps telcos mobile network codes (MCC + MNC) to MoMo network info
type NetworkMapper struct{}

// NewNetworkMapper creates a new network mapper
func NewNetworkMapper() *NetworkMapper {
	return &NetworkMapper{}
}

// MapMobileNetworkCode maps a telco's mobile network code (MCC + MNC) to MoMo network info
func (m *NetworkMapper) MapMobileNetworkCode(MobileNetworkCode string) (*NetworkMapping, error) {
	mapping, ok := NetworkMappings[MobileNetworkCode]
	if !ok {
		return nil, fmt.Errorf("unknown telco network code: %s", MobileNetworkCode)
	}
	return &mapping, nil
}

// GetCountryFromMobileNetworkCode returns the country code for a mobile network code
func (m *NetworkMapper) GetCountryFromMobileNetworkCode(MobileNetworkCode string) string {
	if mapping, ok := NetworkMappings[MobileNetworkCode]; ok {
		return mapping.Country
	}
	return ""
}
