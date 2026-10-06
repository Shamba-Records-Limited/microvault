package phone

// KenyaOperator names the mobile operator a Kenyan number was allocated to.
type KenyaOperator string

// Operators in the Communications Authority numbering plan that matter for
// mobile money. OperatorUnknown covers everything else, including smaller
// operators and numbers that are not Kenyan mobile numbers at all.
const (
	OperatorUnknown   KenyaOperator = ""
	OperatorSafaricom KenyaOperator = "safaricom"
	OperatorAirtel    KenyaOperator = "airtel"
	OperatorTelkom    KenyaOperator = "telkom"
	OperatorEquitel   KenyaOperator = "equitel"
)

// kenyaRange is an inclusive range of three-digit national prefixes, the
// digits after the trunk zero: 0722… is 722, 0110… is 110.
type kenyaRange struct {
	from, to int
	operator KenyaOperator
}

var kenyaRanges = []kenyaRange{
	{100, 109, OperatorAirtel},
	{110, 119, OperatorSafaricom},
	{701, 729, OperatorSafaricom},
	{730, 739, OperatorAirtel},
	{740, 743, OperatorSafaricom},
	{745, 746, OperatorSafaricom},
	{748, 748, OperatorSafaricom},
	{750, 756, OperatorAirtel},
	{757, 759, OperatorSafaricom},
	{762, 762, OperatorAirtel},
	{763, 766, OperatorEquitel},
	{768, 769, OperatorSafaricom},
	{770, 779, OperatorTelkom},
	{780, 789, OperatorAirtel},
	{790, 799, OperatorSafaricom},
}

// KenyaOperatorByPrefix returns the operator a Kenyan mobile number's prefix
// was allocated to. It accepts +254…, 254… and 0… forms with separators.
//
// The answer is the allocation, not the current network: numbers have been
// portable between operators since 2011, so a ported subscriber is reported
// under their old operator. Treat it as a hint and prefer the network the
// gateway reports for a live session.
func KenyaOperatorByPrefix(number string) KenyaOperator {
	digits := make([]byte, 0, len(number))
	for i := 0; i < len(number); i++ {
		if c := number[i]; c >= '0' && c <= '9' {
			digits = append(digits, c)
		}
	}
	national := string(digits)
	switch {
	case len(national) == 12 && national[:3] == "254":
		national = national[3:]
	case len(national) == 10 && national[0] == '0':
		national = national[1:]
	}
	if len(national) != 9 {
		return OperatorUnknown
	}
	prefix := int(national[0]-'0')*100 + int(national[1]-'0')*10 + int(national[2]-'0')
	for _, r := range kenyaRanges {
		if prefix >= r.from && prefix <= r.to {
			return r.operator
		}
	}
	return OperatorUnknown
}
