// Package testing provides the shared test doubles for the stellar packages.
//
// MockRPCClient is a hand-written RPC stub that satisfies both classic.RPCClient
// and soroban.RPCClient. Each method is driven by an optional function hook
// (LoadAccountFunc, SendTransactionFunc, GetTransactionFunc, and so on); when a
// hook is unset the method returns a benign default. Every call is recorded on
// the matching Calls slice so tests can assert what was invoked. Interface
// compliance is asserted in each consumer's own test file rather than here, to
// avoid an import cycle.
package testing
