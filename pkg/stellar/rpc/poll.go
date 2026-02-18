package rpc

import (
	"context"
	"log/slog"
	"time"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"

	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// TransactionGetter is the interface for fetching transaction status
type TransactionGetter interface {
	GetTransaction(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error)
}

// PollConfig configures the polling behavior
type PollConfig struct {
	MaxAttempts  int
	PollInterval time.Duration
	Logger       *slog.Logger
}

// DefaultPollConfig returns the default polling configuration
func DefaultPollConfig() PollConfig {
	return PollConfig{
		MaxAttempts:  10,
		PollInterval: time.Second,
		Logger:       slog.Default(),
	}
}

// PollTransaction polls for transaction completion with proper logging.
// It returns the full GetTransactionResponse on success and any error encountered.
func PollTransaction(
	ctx context.Context,
	client TransactionGetter,
	txHash string,
	cfg PollConfig,
) (protocol.GetTransactionResponse, error) {
	logger := cfg.Logger.With(
		slog.String("tx_hash", txHash),
		slog.String("component", "poll_transaction"),
	)

	logger.Info("starting transaction polling",
		slog.Int("max_attempts", cfg.MaxAttempts),
		slog.Duration("poll_interval", cfg.PollInterval),
	)

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			logger.Warn("context cancelled while polling",
				slog.Int("attempt", attempt),
				slog.String("error", ctx.Err().Error()),
			)
			return protocol.GetTransactionResponse{}, types.ErrContextCancelled
		default:
		}

		// Fetch transaction status
		resp, err := client.GetTransaction(ctx, protocol.GetTransactionRequest{
			Hash: txHash,
		})
		if err != nil {
			logger.Warn("failed to fetch transaction",
				slog.Int("attempt", attempt),
				slog.Int("max_attempts", cfg.MaxAttempts),
				slog.String("error", err.Error()),
			)
			time.Sleep(cfg.PollInterval)
			continue
		}

		logger.Debug("received transaction response",
			slog.Int("attempt", attempt),
			slog.String("status", resp.Status),
			slog.Uint64("ledger", uint64(resp.Ledger)),
		)

		// Handle transaction status
		switch resp.Status {
		case protocol.TransactionStatusSuccess:
			logger.Info("transaction succeeded",
				slog.Int("attempt", attempt),
				slog.Uint64("ledger", uint64(resp.Ledger)),
			)
			return resp, nil

		case protocol.TransactionStatusFailed:
			logger.Error("transaction failed",
				slog.Int("attempt", attempt),
				slog.Uint64("ledger", uint64(resp.Ledger)),
				slog.String("result_xdr", resp.ResultXDR),
			)
			return resp, types.ErrTransactionFailedOnLedger

		case protocol.TransactionStatusNotFound:
			logger.Debug("transaction not yet found",
				slog.Int("attempt", attempt),
				slog.Int("max_attempts", cfg.MaxAttempts),
			)
			time.Sleep(cfg.PollInterval)
			continue

		default:
			logger.Warn("unknown transaction status",
				slog.Int("attempt", attempt),
				slog.String("status", resp.Status),
			)
			return resp, types.ErrUnknownTransactionStatus
		}
	}

	logger.Error("transaction polling timed out",
		slog.Int("max_attempts", cfg.MaxAttempts),
	)
	return protocol.GetTransactionResponse{}, types.ErrTransactionTimeout
}
