// Package sourcelock serialises Stellar submissions per source account, from
// loading the account's sequence number to the transaction's result. Two
// submissions from one account in flight at once load the same sequence
// number, and the second is rejected with txBadSeq.
//
// The process-wide locker defaults to an in-process one; a binary that may
// run beside other processes submitting from the same accounts installs the
// Postgres one at boot with Use.
package sourcelock

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"

	"github.com/Shamba-Records-Limited/microvault/pkg/pglock"
)

// Locker runs fn while holding the lock on a source account.
type Locker interface {
	Hold(ctx context.Context, source string, fn func(context.Context) error) error
}

var (
	current       atomic.Pointer[Locker]
	defaultLocker = NewInProcess()
)

// Use installs the process-wide locker.
func Use(l Locker) { current.Store(&l) }

// Hold runs fn under the process-wide locker, the in-process one unless Use
// installed another.
func Hold(ctx context.Context, source string, fn func(context.Context) error) error {
	if l := current.Load(); l != nil {
		return (*l).Hold(ctx, source, fn)
	}
	return defaultLocker.Hold(ctx, source, fn)
}

// InProcess serialises submissions within this process.
type InProcess struct {
	mu    sync.Mutex
	slots map[string]chan struct{}
}

// NewInProcess returns an in-process locker.
func NewInProcess() *InProcess { return &InProcess{slots: map[string]chan struct{}{}} }

// Hold waits for the account's slot, bounded by ctx.
func (l *InProcess) Hold(ctx context.Context, source string, fn func(context.Context) error) error {
	l.mu.Lock()
	slot, ok := l.slots[source]
	if !ok {
		slot = make(chan struct{}, 1)
		l.slots[source] = slot
	}
	l.mu.Unlock()

	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-slot }()
	return fn(ctx)
}

// Postgres serialises submissions across every process sharing the database.
// Waiting submissions queue in-process first, so a process pins at most one
// connection per account however many are waiting.
type Postgres struct {
	db    *sql.DB
	local *InProcess
}

// NewPostgres returns a locker on db.
func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db, local: NewInProcess()} }

// Hold waits for the account's advisory lock, bounded by ctx.
func (l *Postgres) Hold(ctx context.Context, source string, fn func(context.Context) error) error {
	return l.local.Hold(ctx, source, func(ctx context.Context) error {
		return pglock.Hold(ctx, l.db, "stellar-src:"+source, fn)
	})
}
