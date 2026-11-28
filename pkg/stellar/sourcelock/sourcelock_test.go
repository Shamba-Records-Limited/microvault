package sourcelock

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overlap runs n holders on source at once and reports the most that ran
// together.
func overlap(t *testing.T, lockers []Locker, source string) int32 {
	t.Helper()
	var active, peak atomic.Int32
	var wg sync.WaitGroup
	for i := range 6 {
		l := lockers[i%len(lockers)]
		wg.Go(func() {
			assert.NoError(t, l.Hold(context.Background(), source, func(context.Context) error {
				n := active.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				active.Add(-1)
				return nil
			}))
		})
	}
	wg.Wait()
	return peak.Load()
}

func TestInProcess_SerialisesOneAccount(t *testing.T) {
	assert.Equal(t, int32(1), overlap(t, []Locker{NewInProcess()}, "GSOURCE"))
}

func TestInProcess_OtherAccountsDoNotWait(t *testing.T) {
	l := NewInProcess()
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = l.Hold(context.Background(), "GA", func(context.Context) error { close(held); <-release; return nil })
	}()
	<-held
	defer close(release)
	ran := false
	require.NoError(t, l.Hold(context.Background(), "GB", func(context.Context) error { ran = true; return nil }))
	assert.True(t, ran)
}

func TestInProcess_WaitEndsWithTheContext(t *testing.T) {
	l := NewInProcess()
	held, release := make(chan struct{}), make(chan struct{})
	go func() {
		_ = l.Hold(context.Background(), "GA", func(context.Context) error { close(held); <-release; return nil })
	}()
	<-held
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, l.Hold(ctx, "GA", func(context.Context) error { t.Error("ran while held"); return nil }), context.DeadlineExceeded)
}

// Two processes are two Postgres lockers on separate pools.
func TestPostgres_SerialisesAcrossProcesses(t *testing.T) {
	dsn := os.Getenv("PGLOCK_TEST_DSN")
	if dsn == "" {
		t.Skip("set PGLOCK_TEST_DSN to run against Postgres")
	}
	open := func() *sql.DB {
		db, err := sql.Open("pgx", dsn)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	assert.Equal(t, int32(1), overlap(t, []Locker{NewPostgres(open()), NewPostgres(open())}, "GSOURCE-"+t.Name()))
}

// Waiting submissions queue in-process, so they pin no extra connections.
func TestPostgres_WaitersPinNoConnections(t *testing.T) {
	dsn := os.Getenv("PGLOCK_TEST_DSN")
	if dsn == "" {
		t.Skip("set PGLOCK_TEST_DSN to run against Postgres")
	}
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	l := NewPostgres(db)

	held, release, failed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		if err := l.Hold(context.Background(), "GPIN", func(context.Context) error { close(held); <-release; return nil }); err != nil {
			failed <- err
		}
	}()
	select {
	case <-held:
	case err := <-failed:
		t.Fatalf("the holder could not take the lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the holder never took the lock")
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() { _ = l.Hold(context.Background(), "GPIN", func(context.Context) error { return nil }) })
	}
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, db.Stats().InUse, "only the holder pins a connection")
	close(release)
	wg.Wait()
}

func TestUse_InstallsTheProcessLocker(t *testing.T) {
	t.Cleanup(func() { current.Store(nil) })
	calls := 0
	Use(lockerFunc(func(ctx context.Context, _ string, fn func(context.Context) error) error { calls++; return fn(ctx) }))
	require.NoError(t, Hold(context.Background(), "GA", func(context.Context) error { return nil }))
	assert.Equal(t, 1, calls)
}

type lockerFunc func(ctx context.Context, source string, fn func(context.Context) error) error

func (f lockerFunc) Hold(ctx context.Context, source string, fn func(context.Context) error) error {
	return f(ctx, source, fn)
}
