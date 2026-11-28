package pglock

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// Leader runs work only while it holds a session lock, so of several
// processes campaigning on the same key exactly one does the work. The lock's
// connection is pinged while held; if it fails, Postgres has released the
// lock, so work is cancelled and waited for before campaigning again.
type Leader struct {
	DB  *sql.DB
	Key string
	// Retry spaces attempts while another process leads (default 5s).
	Retry time.Duration
	// Ping spaces checks of the held connection (default 5s). It bounds how
	// long a leader that lost its connection keeps working.
	Ping   time.Duration
	Logger *slog.Logger
}

// Run campaigns until ctx ends. work must return once its context is
// cancelled, after everything it started has stopped.
func (l *Leader) Run(ctx context.Context, work func(context.Context)) {
	retry, ping, logger := l.Retry, l.Ping, l.Logger
	if retry <= 0 {
		retry = 5 * time.Second
	}
	if ping <= 0 {
		ping = 5 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("lock_key", l.Key)

	standby := false
	for ctx.Err() == nil {
		led, err := l.lead(ctx, ping, logger, work)
		switch {
		case err != nil:
			logger.WarnContext(ctx, "leader campaign failed", "error", err)
		case !led && !standby:
			logger.InfoContext(ctx, "another process leads; standing by")
		}
		standby = !led && err == nil
		select {
		case <-ctx.Done():
		case <-time.After(retry):
		}
	}
}

// lead takes the lock if it is free and works until ctx ends or the
// connection fails. led is false when another process holds the lock.
func (l *Leader) lead(ctx context.Context, ping time.Duration, logger *slog.Logger, work func(context.Context)) (led bool, err error) {
	conn, err := l.DB.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()

	acquired, err := lock(ctx, conn, l.Key, false)
	if err != nil || !acquired {
		return false, err
	}
	logger.InfoContext(ctx, "leading")

	workCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		work(workCtx)
	}()

	ticker := time.NewTicker(ping)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			cancel()
			release(ctx, conn, l.Key)
			return true, nil
		case <-ctx.Done():
			cancel()
			<-done
			release(ctx, conn, l.Key)
			return true, nil
		case <-ticker.C:
			if err := conn.PingContext(ctx); err != nil && ctx.Err() == nil {
				logger.ErrorContext(ctx, "leader lock connection lost; stopping work", "error", err)
				cancel()
				<-done
				discard(conn)
				return true, nil
			}
		}
	}
}
