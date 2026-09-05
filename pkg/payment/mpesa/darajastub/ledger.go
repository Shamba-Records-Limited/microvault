package darajastub

import (
	"fmt"
	"sync"
)

// Account names one of the four books M-Pesa keeps under a shortcode.
type Account string

// The shortcode accounts. Which one an API touches is not interchangeable:
// C2B credits Utility, B2C debits Utility, and B2B debits MMF/Working, so a
// disbursement mix can drain one while the other looks healthy.
const (
	AccountUtility     Account = "utility"
	AccountWorking     Account = "working"
	AccountChargesPaid Account = "charges_paid"
	AccountSettlement  Account = "settlement"
)

// Amounts are held in minor units — cents — because that is what Daraja itself
// reports in the MinimumAmount field, and because floating point has no place
// in a ledger.
type ledger struct {
	mu       sync.Mutex
	balances map[uint]map[Account]int64
}

func newLedger() *ledger {
	return &ledger{balances: make(map[uint]map[Account]int64)}
}

func (l *ledger) get(shortcode uint, account Account) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.balances[shortcode][account]
}

func (l *ledger) credit(shortcode uint, account Account, minor int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensure(shortcode)
	l.balances[shortcode][account] += minor
}

// debit removes minor units, reporting whether the balance covered it. A
// shortfall leaves the balance untouched so ResultCode 1 happens because the
// money is genuinely absent rather than because a test asked for it.
func (l *ledger) debit(shortcode uint, account Account, minor int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensure(shortcode)
	if l.balances[shortcode][account] < minor {
		return false
	}
	l.balances[shortcode][account] -= minor
	return true
}

func (l *ledger) ensure(shortcode uint) {
	if _, ok := l.balances[shortcode]; !ok {
		l.balances[shortcode] = make(map[Account]int64)
	}
}

// Balance reports a shortcode account balance in minor units.
func (s *Stub) Balance(shortcode uint, account Account) int64 {
	return s.ledger.get(shortcode, account)
}

// Credit adds minor units to a shortcode account, for arranging a test's
// opening position.
func (s *Stub) Credit(shortcode uint, account Account, minor int64) {
	s.ledger.credit(shortcode, account, minor)
}

// Transaction is one movement the stub recorded.
type Transaction struct {
	ID        string
	Shortcode uint
	Account   Account
	Minor     int64
	MSISDN    string
	Reference string
	Kind      string
}

// Transactions returns every recorded movement, oldest first.
func (s *Stub) Transactions() []Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Transaction(nil), s.transactions...)
}

func (s *Stub) record(tx Transaction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transactions = append(s.transactions, tx)
}

// formatBalance renders a balance the way Daraja does in a Result envelope.
func formatBalance(minor int64) string {
	return fmt.Sprintf("%d.%02d", minor/100, abs(minor%100))
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
