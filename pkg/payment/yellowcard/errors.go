package yellowcard

import "fmt"

// InsufficientBalanceError is returned when the YellowCard account balance
// is too low for a fiat disbursement.
type InsufficientBalanceError struct {
	Available float64
	Requested float64
}

func (e *InsufficientBalanceError) Error() string {
	return fmt.Sprintf("insufficient YC balance: available %.2f USD, requested %.2f USD",
		e.Available, e.Requested)
}

// NetworkNotFoundError is returned when a network is not found in YellowCard.
type NetworkNotFoundError struct {
	NetworkCode string
	NetworkName string
	Country     string
}

func (e *NetworkNotFoundError) Error() string {
	return fmt.Sprintf("network '%s' (%s) not found in country %s", e.NetworkName, e.NetworkCode, e.Country)
}

// NetworkInactiveError is returned when a network is not currently active.
type NetworkInactiveError struct {
	NetworkCode string
	NetworkName string
	Country     string
	Status      string
}

func (e *NetworkInactiveError) Error() string {
	return fmt.Sprintf("network '%s' (%s) is currently %s in country %s", e.NetworkName, e.NetworkCode, e.Status, e.Country)
}
