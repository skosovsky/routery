package routerysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type testDriverConfig struct {
	block    bool
	execErr  error
	queryErr error
}

type testDriverState struct {
	beginCalls      int
	commitCalls     int
	execCalls       int
	execStatements  []string
	execValues      [][]any
	mu              sync.Mutex
	queryCalls      int
	queryStatements []string
	queryValues     [][]any
	rollbackCalls   int
	rowsClosed      int
}

func (state *testDriverState) beginCallCount() int {
	state.mu.Lock()
	defer state.mu.Unlock()

	return state.beginCalls
}

func (state *testDriverState) commitCallCount() int {
	state.mu.Lock()
	defer state.mu.Unlock()

	return state.commitCalls
}

func (state *testDriverState) execCallCount() int {
	state.mu.Lock()
	defer state.mu.Unlock()

	return state.execCalls
}

func (state *testDriverState) lastExec() (string, []any) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.execStatements) == 0 {
		return "", nil
	}

	statement := state.execStatements[len(state.execStatements)-1]
	values := append([]any(nil), state.execValues[len(state.execValues)-1]...)
	return statement, values
}

func (state *testDriverState) lastQuery() (string, []any) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.queryStatements) == 0 {
		return "", nil
	}

	statement := state.queryStatements[len(state.queryStatements)-1]
	values := append([]any(nil), state.queryValues[len(state.queryValues)-1]...)
	return statement, values
}

func (state *testDriverState) queryCallCount() int {
	state.mu.Lock()
	defer state.mu.Unlock()

	return state.queryCalls
}

func (state *testDriverState) rollbackCallCount() int {
	state.mu.Lock()
	defer state.mu.Unlock()

	return state.rollbackCalls
}

func (state *testDriverState) rowsClosedCount() int {
	state.mu.Lock()
	defer state.mu.Unlock()

	return state.rowsClosed
}

func (state *testDriverState) recordBegin() {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.beginCalls++
}

func (state *testDriverState) recordCommit() {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.commitCalls++
}

func (state *testDriverState) recordExec(statement string, args []driver.NamedValue) {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.execCalls++
	state.execStatements = append(state.execStatements, statement)
	state.execValues = append(state.execValues, namedValuesToAny(args))
}

func (state *testDriverState) recordQuery(statement string, args []driver.NamedValue) {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.queryCalls++
	state.queryStatements = append(state.queryStatements, statement)
	state.queryValues = append(state.queryValues, namedValuesToAny(args))
}

func (state *testDriverState) recordRollback() {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.rollbackCalls++
}

func (state *testDriverState) recordRowsClosed() {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.rowsClosed++
}

func openTestDB(tb testing.TB, config testDriverConfig) (*sql.DB, *testDriverState) {
	tb.Helper()

	db, state, err := openTestDBWithName(uniqueDriverName(tb.Name()), config)
	if err != nil {
		tb.Fatalf("failed to open test DB: %v", err)
	}
	tb.Cleanup(func() {
		_ = db.Close()
	})

	return db, state
}

func openTestDBWithName(name string, config testDriverConfig) (*sql.DB, *testDriverState, error) {
	state := &testDriverState{}
	sql.Register(name, &testDriver{
		config: config,
		state:  state,
	})

	db, err := sql.Open(name, "")
	if err != nil {
		return nil, nil, err
	}

	return db, state, nil
}

func uniqueDriverName(name string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_")
	return fmt.Sprintf("routerysql_test_driver_%s_%d", replacer.Replace(name), time.Now().UnixNano())
}

func namedValuesToAny(args []driver.NamedValue) []any {
	values := make([]any, len(args))
	for index, arg := range args {
		values[index] = arg.Value
	}
	return values
}

type testDriver struct {
	config testDriverConfig
	state  *testDriverState
}

func (driverImplementation *testDriver) Open(string) (driver.Conn, error) {
	return &testConn{
		config: driverImplementation.config,
		state:  driverImplementation.state,
	}, nil
}

type testConn struct {
	config testDriverConfig
	state  *testDriverState
}

func (conn *testConn) Begin() (driver.Tx, error) {
	return conn.BeginTx(context.Background(), driver.TxOptions{})
}

func (conn *testConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if conn.config.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	conn.state.recordBegin()
	return &testTx{
		state: conn.state,
	}, nil
}

func (conn *testConn) Close() error {
	return nil
}

func (conn *testConn) ExecContext(
	ctx context.Context,
	statement string,
	args []driver.NamedValue,
) (driver.Result, error) {
	if conn.config.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	if conn.config.execErr != nil {
		return nil, conn.config.execErr
	}

	conn.state.recordExec(statement, args)
	return driver.RowsAffected(1), nil
}

func (conn *testConn) Prepare(string) (driver.Stmt, error) {
	return &testStmt{}, nil
}

func (conn *testConn) QueryContext(
	ctx context.Context,
	statement string,
	args []driver.NamedValue,
) (driver.Rows, error) {
	if conn.config.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	if conn.config.queryErr != nil {
		return nil, conn.config.queryErr
	}

	conn.state.recordQuery(statement, args)
	return &testRows{
		state: conn.state,
	}, nil
}

type testRows struct {
	closed  bool
	emitted bool
	state   *testDriverState
}

func (rows *testRows) Close() error {
	if rows.closed {
		return nil
	}

	rows.closed = true
	rows.state.recordRowsClosed()
	return nil
}

func (rows *testRows) Columns() []string {
	return []string{"value"}
}

func (rows *testRows) Next(destination []driver.Value) error {
	if rows.emitted {
		return io.EOF
	}

	rows.emitted = true
	if len(destination) > 0 {
		destination[0] = "ok"
	}

	return nil
}

type testStmt struct{}

func (stmt *testStmt) Close() error {
	return nil
}

func (stmt *testStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("prepared statements are not supported in tests")
}

func (stmt *testStmt) NumInput() int {
	return -1
}

func (stmt *testStmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("prepared statements are not supported in tests")
}

type testTx struct {
	state *testDriverState
}

func (tx *testTx) Commit() error {
	tx.state.recordCommit()
	return nil
}

func (tx *testTx) Rollback() error {
	tx.state.recordRollback()
	return nil
}
