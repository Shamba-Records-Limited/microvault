// Package pglock serialises work across processes with Postgres advisory
// locks. Keys are strings hashed with hashtext, the form the poller runners
// have always used, so a key keeps meaning the same lock across versions.
package pglock

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"time"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

func lockErr(op, key string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainPersistence).Tags("pglock").With(pkgErrors.AttrOperation, op).With("lock_key", key)
}

// TryXact runs fn while holding a transaction-scoped lock on key and reports
// whether it ran. The lock is released when the transaction ends, so it needs
// no pinned connection; the transaction is always rolled back, since it
// exists only to scope the lock. Use it for work that fits one transaction's
// lifetime, such as a poller tick.
func TryXact(ctx context.Context, db *sql.DB, key string, fn func(context.Context)) (ran bool, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, lockErr("try_xact", key).Code(pkgErrors.CodeLockFailed).Wrapf(err, "could not open the lock transaction")
	}
	defer func() { _ = tx.Rollback() }()

	var acquired bool
	if err := tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", key).Scan(&acquired); err != nil {
		return false, lockErr("try_xact", key).Code(pkgErrors.CodeLockFailed).Wrapf(err, "could not take the lock")
	}
	if !acquired {
		return false, nil
	}
	fn(ctx)
	return true, nil
}

// TryHold runs fn while holding a session lock on key, on a connection
// pinned for the duration, and reports whether it ran. Use it for work that
// spans network calls, where holding a transaction open would be wrong.
func TryHold(ctx context.Context, db *sql.DB, key string, fn func(context.Context)) (ran bool, err error) {
	return hold(ctx, db, key, false, func(ctx context.Context) error { fn(ctx); return nil })
}

// Hold waits for the session lock on key, bounded by ctx, then runs fn and
// returns its error.
func Hold(ctx context.Context, db *sql.DB, key string, fn func(context.Context) error) error {
	_, err := hold(ctx, db, key, true, fn)
	return err
}

func hold(ctx context.Context, db *sql.DB, key string, wait bool, fn func(context.Context) error) (ran bool, err error) {
	op := "try_hold"
	if wait {
		op = "hold"
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return false, lockErr(op, key).Code(pkgErrors.CodeLockFailed).Wrapf(err, "could not pin a connection")
	}
	defer func() { _ = conn.Close() }()

	acquired, err := lock(ctx, conn, key, wait)
	if err != nil {
		return false, lockErr(op, key).Code(pkgErrors.CodeLockFailed).Wrapf(err, "could not take the lock")
	}
	if !acquired {
		return false, nil
	}
	defer release(ctx, conn, key)
	return true, fn(ctx)
}

// lock takes the session lock on conn, waiting for it when wait is set. A
// failed or cancelled attempt drops the connection: the lock may have been
// granted as the attempt was abandoned.
func lock(ctx context.Context, conn *sql.Conn, key string, wait bool) (bool, error) {
	if wait {
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(hashtext($1))", key); err != nil {
			discard(conn)
			return false, err
		}
		return true, nil
	}
	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", key).Scan(&acquired); err != nil {
		discard(conn)
		return false, err
	}
	return acquired, nil
}

const releaseTimeout = 5 * time.Second

// release unlocks key, or drops the connection so Postgres frees the lock
// when the unlock cannot be confirmed. A pooled connection still holding the
// lock would block every later holder. It runs after the work's context may
// have ended, so it ignores that cancellation and is bounded on its own.
func release(ctx context.Context, conn *sql.Conn, key string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	var unlocked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock(hashtext($1))", key).Scan(&unlocked); err != nil || !unlocked {
		discard(conn)
	}
}

func discard(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}
