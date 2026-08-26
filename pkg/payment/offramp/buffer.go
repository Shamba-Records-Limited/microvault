package offramp

// RateBuffer is a fractional safety margin deducted from a quoted FX rate to
// hedge drift between the moment a borrower is quoted and the moment the
// off-ramp settles.
type RateBuffer struct {
	pct float64
}

// NewRateBuffer resolves a configured fraction against a default.
//
// pct is a pointer because zero is a meaningful setting — quote raw, no
// margin — and has to stay distinguishable from "not configured", which takes
// def instead.
func NewRateBuffer(pct *float64, def float64) RateBuffer {
	if pct == nil {
		return RateBuffer{pct: def}
	}
	return RateBuffer{pct: *pct}
}

// Pct returns the resolved fraction, where 0.01 is 1 %. Zero means rates are
// quoted raw. Persist it alongside the rate so drift is auditable after the
// fact.
func (b RateBuffer) Pct() float64 { return b.pct }

// Apply returns rate reduced by the margin, clamped at zero.
func (b RateBuffer) Apply(rate float64) float64 {
	r := rate * (1.0 - b.pct)
	if r < 0 {
		return 0
	}
	return r
}

// Fraction returns a pointer to f, for populating optional buffer settings
// from literals in configuration and tests.
func Fraction(f float64) *float64 { return &f }
