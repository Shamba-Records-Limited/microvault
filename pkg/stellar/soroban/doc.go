// Package soroban is the Go client for the Vault smart contract. It invokes
// contract functions over Soroban RPC and decodes their results back into Go
// values.
//
// Service is the single interface, grouped into three kinds of call. View
// functions (GetTreasuryAddress, GetTotalBorrowed, GetAvailableLiquidity,
// IsPaused, and the rest) are read-only. Treasury operations (BorrowFromVault,
// RepayToVault, AccrueInterest) move funds and are signed by the treasury key.
// Admin operations (PauseVault, SetLockPeriod, and the other setters) reconfigure
// the contract and are signed by the admin key. Build a Service with NewService,
// or NewServiceWithClient to supply your own RPCClient (the seam the tests use).
//
// # Invocation shape
//
// Every contract call follows the same internal path. Go arguments are encoded
// to ScVal (see the helpers in helpers.go), wrapped in an InvokeHostFunction
// operation, and simulated. Simulation returns the authorization entries,
// resource fee, and Soroban transaction data. View functions stop there and
// decode the simulated return value. Mutating calls carry the simulation output
// onto a real transaction, sign it with the appropriate key, submit it, and poll
// via rpc.PollTransaction — a PENDING submit is acceptance, not confirmation.
//
// # Two keys
//
// The treasury key authorizes contract calls that move value on the treasury's
// behalf; the admin key authorizes owner-only configuration. Read-only views may
// be simulated under either identity since simulation never alters state.
//
// Contract-level failures are mapped to typed errors by the types package; this
// package returns those for vault errors and wraps lower-level encoding or RPC
// failures with context.
package soroban
