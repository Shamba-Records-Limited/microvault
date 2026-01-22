package testing

import (
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// ============================================================================
// Simulation Response Builders
// ============================================================================

// SimulationResponseBuilder helps construct mock simulation responses
type SimulationResponseBuilder struct {
	resp protocol.SimulateTransactionResponse
}

// NewSimulationResponse creates a new simulation response builder
func NewSimulationResponse() *SimulationResponseBuilder {
	return &SimulationResponseBuilder{
		resp: protocol.SimulateTransactionResponse{
			Results: []protocol.SimulateHostFunctionResult{},
		},
	}
}

// WithError sets an error on the simulation response
func (b *SimulationResponseBuilder) WithError(err string) *SimulationResponseBuilder {
	b.resp.Error = err
	return b
}

// WithMinResourceFee sets the minimum resource fee
func (b *SimulationResponseBuilder) WithMinResourceFee(fee int64) *SimulationResponseBuilder {
	b.resp.MinResourceFee = fee
	return b
}

// WithI128Result adds an i128 result to the simulation
func (b *SimulationResponseBuilder) WithI128Result(value int64) *SimulationResponseBuilder {
	var hi int64 = 0
	if value < 0 {
		hi = -1
	}

	scVal := xdr.ScVal{
		Type: xdr.ScValTypeScvI128,
		I128: &xdr.Int128Parts{
			Hi: xdr.Int64(hi),
			Lo: xdr.Uint64(value),
		},
	}

	return b.withScValResult(scVal)
}

// WithBoolResult adds a bool result to the simulation
func (b *SimulationResponseBuilder) WithBoolResult(value bool) *SimulationResponseBuilder {
	scVal := xdr.ScVal{
		Type: xdr.ScValTypeScvBool,
		B:    &value,
	}

	return b.withScValResult(scVal)
}

// WithU64Result adds a u64 result to the simulation
func (b *SimulationResponseBuilder) WithU64Result(value uint64) *SimulationResponseBuilder {
	val := xdr.Uint64(value)
	scVal := xdr.ScVal{
		Type: xdr.ScValTypeScvU64,
		U64:  &val,
	}

	return b.withScValResult(scVal)
}

// WithAddressResult adds an address result to the simulation
func (b *SimulationResponseBuilder) WithAddressResult(address string) *SimulationResponseBuilder {
	accountID := xdr.MustAddress(address)
	scAddr, _ := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeAccount, accountID)

	scVal := xdr.ScVal{
		Type:    xdr.ScValTypeScvAddress,
		Address: &scAddr,
	}

	return b.withScValResult(scVal)
}

// WithTransactionData adds mock transaction data for submission
func (b *SimulationResponseBuilder) WithTransactionData() *SimulationResponseBuilder {
	// Create minimal valid SorobanTransactionData
	txData := xdr.SorobanTransactionData{
		Resources: xdr.SorobanResources{
			Footprint: xdr.LedgerFootprint{
				ReadOnly:  []xdr.LedgerKey{},
				ReadWrite: []xdr.LedgerKey{},
			},
			Instructions:  100000,
			DiskReadBytes: 1000,
			WriteBytes:    1000,
		},
		ResourceFee: 10000,
	}

	txDataBytes, _ := xdr.MarshalBase64(txData)
	b.resp.TransactionDataXDR = txDataBytes

	return b
}

// WithAuth adds mock auth entries for transaction signing
func (b *SimulationResponseBuilder) WithAuth() *SimulationResponseBuilder {
	// Create a mock contract address for the auth entry
	var contractId xdr.ContractId
	// Use a deterministic mock contract ID (32 bytes of 0x01)
	for i := range contractId {
		contractId[i] = 0x01
	}
	contractAddr, _ := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeContract, contractId)

	// Create minimal auth entry
	auth := xdr.SorobanAuthorizationEntry{
		Credentials: xdr.SorobanCredentials{
			Type: xdr.SorobanCredentialsTypeSorobanCredentialsSourceAccount,
		},
		RootInvocation: xdr.SorobanAuthorizedInvocation{
			Function: xdr.SorobanAuthorizedFunction{
				Type: xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeContractFn,
				ContractFn: &xdr.InvokeContractArgs{
					ContractAddress: contractAddr,
					FunctionName:    "test",
					Args:            []xdr.ScVal{},
				},
			},
			SubInvocations: []xdr.SorobanAuthorizedInvocation{},
		},
	}

	authBytes, _ := xdr.MarshalBase64(auth)
	authSlice := []string{authBytes}

	if len(b.resp.Results) == 0 {
		b.resp.Results = []protocol.SimulateHostFunctionResult{{}}
	}
	b.resp.Results[0].AuthXDR = &authSlice

	return b
}

func (b *SimulationResponseBuilder) withScValResult(scVal xdr.ScVal) *SimulationResponseBuilder {
	scValBytes, err := xdr.MarshalBase64(scVal)
	if err != nil {
		panic("failed to marshal ScVal: " + err.Error())
	}

	if len(b.resp.Results) == 0 {
		b.resp.Results = []protocol.SimulateHostFunctionResult{}
	}

	b.resp.Results = append(b.resp.Results, protocol.SimulateHostFunctionResult{
		ReturnValueXDR: &scValBytes,
	})

	return b
}

// Build returns the constructed simulation response
func (b *SimulationResponseBuilder) Build() protocol.SimulateTransactionResponse {
	return b.resp
}

// ============================================================================
// Transaction Response Builders
// ============================================================================

// SendTransactionResponseBuilder helps construct mock send transaction responses
type SendTransactionResponseBuilder struct {
	resp protocol.SendTransactionResponse
}

// NewSendTransactionResponse creates a new send transaction response builder
func NewSendTransactionResponse() *SendTransactionResponseBuilder {
	return &SendTransactionResponseBuilder{
		resp: protocol.SendTransactionResponse{
			Status: "PENDING",
			Hash:   "mock_tx_hash_" + randomHash(),
		},
	}
}

// WithStatus sets the transaction status
func (b *SendTransactionResponseBuilder) WithStatus(status string) *SendTransactionResponseBuilder {
	b.resp.Status = status
	return b
}

// WithHash sets the transaction hash
func (b *SendTransactionResponseBuilder) WithHash(hash string) *SendTransactionResponseBuilder {
	b.resp.Hash = hash
	return b
}

// WithError sets an error on the response
func (b *SendTransactionResponseBuilder) WithError(errXDR string) *SendTransactionResponseBuilder {
	b.resp.ErrorResultXDR = errXDR
	return b
}

// Build returns the constructed response
func (b *SendTransactionResponseBuilder) Build() protocol.SendTransactionResponse {
	return b.resp
}

// ============================================================================
// Get Transaction Response Builders
// ============================================================================

// GetTransactionResponseBuilder helps construct mock get transaction responses
type GetTransactionResponseBuilder struct {
	resp protocol.GetTransactionResponse
}

// NewGetTransactionResponse creates a new get transaction response builder
func NewGetTransactionResponse() *GetTransactionResponseBuilder {
	builder := &GetTransactionResponseBuilder{
		resp: protocol.GetTransactionResponse{
			LatestLedger: 12345,
		},
	}
	builder.resp.Status = protocol.TransactionStatusSuccess
	builder.resp.Ledger = 12345
	return builder
}

// WithStatus sets the transaction status
func (b *GetTransactionResponseBuilder) WithStatus(status string) *GetTransactionResponseBuilder {
	b.resp.Status = status
	return b
}

// WithLedger sets the ledger number
func (b *GetTransactionResponseBuilder) WithLedger(ledger uint32) *GetTransactionResponseBuilder {
	b.resp.Ledger = ledger
	return b
}

// WithResultXDR sets the result XDR
func (b *GetTransactionResponseBuilder) WithResultXDR(resultXDR string) *GetTransactionResponseBuilder {
	b.resp.ResultXDR = resultXDR
	return b
}

// Build returns the constructed response
func (b *GetTransactionResponseBuilder) Build() protocol.GetTransactionResponse {
	return b.resp
}

// ============================================================================
// Account Response Builder
// ============================================================================

// AccountBuilder helps construct mock account responses
type AccountBuilder struct {
	account txnbuild.SimpleAccount
}

// NewAccount creates a new account builder
func NewAccount(address string) *AccountBuilder {
	return &AccountBuilder{
		account: txnbuild.SimpleAccount{
			AccountID: address,
			Sequence:  12345,
		},
	}
}

// WithSequence sets the account sequence
func (b *AccountBuilder) WithSequence(seq int64) *AccountBuilder {
	b.account.Sequence = seq
	return b
}

// Build returns the constructed account
func (b *AccountBuilder) Build() *txnbuild.SimpleAccount {
	return &b.account
}

// ============================================================================
// Helpers
// ============================================================================

func randomHash() string {
	return "0123456789abcdef0123456789abcdef"
}
