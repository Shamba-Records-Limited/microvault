package testing

import (
	"context"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// MockRPCClient implements both classic.RPCClient and soroban.RPCClient interfaces.
// Interface compliance is verified in the respective test files to avoid import cycles.

// MockRPCClient is a mock implementation of the Stellar RPC client for testing
type MockRPCClient struct {
	// Function hooks - set these to customize behavior in tests
	LoadAccountFunc         func(ctx context.Context, address string) (txnbuild.Account, error)
	SimulateTransactionFunc func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error)
	SendTransactionFunc     func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error)
	GetTransactionFunc      func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error)
	GetLedgerEntriesFunc    func(ctx context.Context, req protocol.GetLedgerEntriesRequest) (protocol.GetLedgerEntriesResponse, error)

	// Call tracking
	LoadAccountCalls         []string
	SimulateTransactionCalls []protocol.SimulateTransactionRequest
	SendTransactionCalls     []protocol.SendTransactionRequest
	GetTransactionCalls      []protocol.GetTransactionRequest
}

// NewMockRPCClient creates a new mock RPC client with default implementations
func NewMockRPCClient() *MockRPCClient {
	return &MockRPCClient{
		LoadAccountCalls:         make([]string, 0),
		SimulateTransactionCalls: make([]protocol.SimulateTransactionRequest, 0),
		SendTransactionCalls:     make([]protocol.SendTransactionRequest, 0),
		GetTransactionCalls:      make([]protocol.GetTransactionRequest, 0),
	}
}

// LoadAccount mocks the LoadAccount method
func (m *MockRPCClient) LoadAccount(ctx context.Context, address string) (txnbuild.Account, error) {
	m.LoadAccountCalls = append(m.LoadAccountCalls, address)
	if m.LoadAccountFunc != nil {
		return m.LoadAccountFunc(ctx, address)
	}
	// Default: return a valid account using SimpleAccount
	return &txnbuild.SimpleAccount{
		AccountID: address,
		Sequence:  12345,
	}, nil
}

// SimulateTransaction mocks the SimulateTransaction method
func (m *MockRPCClient) SimulateTransaction(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
	m.SimulateTransactionCalls = append(m.SimulateTransactionCalls, req)
	if m.SimulateTransactionFunc != nil {
		return m.SimulateTransactionFunc(ctx, req)
	}
	// Default: return empty successful simulation
	return protocol.SimulateTransactionResponse{}, nil
}

// SendTransaction mocks the SendTransaction method
func (m *MockRPCClient) SendTransaction(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
	m.SendTransactionCalls = append(m.SendTransactionCalls, req)
	if m.SendTransactionFunc != nil {
		return m.SendTransactionFunc(ctx, req)
	}
	// Default: return pending status
	return protocol.SendTransactionResponse{
		Status: "PENDING",
		Hash:   "mock_tx_hash_abc123",
	}, nil
}

// GetTransaction mocks the GetTransaction method
func (m *MockRPCClient) GetTransaction(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
	m.GetTransactionCalls = append(m.GetTransactionCalls, req)
	if m.GetTransactionFunc != nil {
		return m.GetTransactionFunc(ctx, req)
	}
	// Default: return successful transaction
	resp := protocol.GetTransactionResponse{
		LatestLedger: 12345,
	}
	resp.Status = protocol.TransactionStatusSuccess
	resp.Ledger = 12345
	return resp, nil
}

// GetLedgerEntries mocks the GetLedgerEntries method
func (m *MockRPCClient) GetLedgerEntries(ctx context.Context, req protocol.GetLedgerEntriesRequest) (protocol.GetLedgerEntriesResponse, error) {
	if m.GetLedgerEntriesFunc != nil {
		return m.GetLedgerEntriesFunc(ctx, req)
	}
	// Default: return empty entries
	return protocol.GetLedgerEntriesResponse{
		Entries: []protocol.LedgerEntryResult{},
	}, nil
}

// Reset clears all call tracking
func (m *MockRPCClient) Reset() {
	m.LoadAccountCalls = make([]string, 0)
	m.SimulateTransactionCalls = make([]protocol.SimulateTransactionRequest, 0)
	m.SendTransactionCalls = make([]protocol.SendTransactionRequest, 0)
	m.GetTransactionCalls = make([]protocol.GetTransactionRequest, 0)
}

// ============================================================================
// Test Fixtures
// ============================================================================

// TestKeys contains test keypairs for testing
type TestKeys struct {
	TreasurySecret string
	TreasuryPublic string
	AdminSecret    string
	AdminPublic    string
	UserSecret     string
	UserPublic     string
	ContractID     string
}

// NewTestKeys returns a set of valid test keys
// These are deterministic keys for testing only
func NewTestKeys() *TestKeys {
	return &TestKeys{
		// Valid Stellar testnet keypairs
		TreasurySecret: "SAN5L2CY3B57KZHWQ27KSO4DBRN7OLF4WON4REWDZYKR7A37OR4FQSTH",
		TreasuryPublic: "GAFJ5Y3VODHLM6KCGIVOUHJEPHSAUREWCPYYRDJIOO526UJQ4RPSY26E",
		AdminSecret:    "SCSX7IXAUA4ND4JLSVNVIOMZCPJTEAAAOY5PH7YK22AW6RDYNI3GZGSQ",
		AdminPublic:    "GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX",
		UserSecret:     "SBQ3WKKCUHMX42MHWJDBFZMXN6VGVBDZU3XJJWMWWTQPNM6FQDI2TALB",
		UserPublic:     "GDLGMIMK7MM6YHJIPPRAVZSCFYJWE735GJ3BONBWROGW3LFGXSYL3RLB",
		ContractID:     "CA7HVINR2AY532D7UPOE7MMOZAN2ZBZ327VWIO33K7JM7C4G6XYX5NOT",
	}
}

// TestNetworkPassphrase is the passphrase used for testing
const TestNetworkPassphrase = "Test SDF Network ; September 2015"
