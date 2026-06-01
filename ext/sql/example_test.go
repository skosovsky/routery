package routerysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/skosovsky/routery"
)

func ExampleNewDBQueryHandler() {
	db, state, err := openTestDBWithName(uniqueDriverName("example-query"), testDriverConfig{})
	if err != nil {
		fmt.Println("unexpected", err)
		return
	}
	defer db.Close()

	type queryRequest struct {
		ID int
	}

	executor := NewDBQueryHandler(
		db,
		func(_ context.Context, req queryRequest) (string, []any, error) {
			return "SELECT value FROM widgets WHERE id = ?", []any{req.ID}, nil
		},
	)

	rowsResult, executeErr := executor.Handle(context.Background(), queryRequest{ID: 1})
	if executeErr != nil {
		fmt.Println("unexpected", executeErr)
		return
	}
	if rowsErr := drainRowsForExample(rowsResult.Payload); rowsErr != nil {
		fmt.Println("unexpected", rowsErr)
		return
	}

	fmt.Println(state.queryCallCount())
	// Output: 1
}

func ExampleWeightBasedRouter_masterReplica() {
	primaryDB, primaryState, primaryErr := openTestDBWithName(
		uniqueDriverName("example-primary"),
		testDriverConfig{},
	)
	if primaryErr != nil {
		fmt.Println("unexpected", primaryErr)
		return
	}
	defer primaryDB.Close()

	replicaDB, replicaState, replicaErr := openTestDBWithName(
		uniqueDriverName("example-replica"),
		testDriverConfig{},
	)
	if replicaErr != nil {
		fmt.Println("unexpected", replicaErr)
		return
	}
	defer replicaDB.Close()

	type queryRequest struct {
		SQL string
	}

	primaryExecutor := NewDBQueryHandler(
		primaryDB,
		func(_ context.Context, req queryRequest) (string, []any, error) {
			return req.SQL, nil, nil
		},
	)
	replicaExecutor := NewDBQueryHandler(
		replicaDB,
		func(_ context.Context, req queryRequest) (string, []any, error) {
			return req.SQL, nil, nil
		},
	)

	router := routery.WeightBasedRouter(
		func(_ context.Context, req queryRequest) (int, error) {
			return len(strings.Fields(req.SQL)), nil
		},
		5,
		replicaExecutor,
		primaryExecutor,
	)

	shortRowsResult, shortErr := router.Handle(context.Background(), queryRequest{
		SQL: "SELECT value FROM widgets",
	})
	if shortErr != nil {
		fmt.Println("unexpected", shortErr)
		return
	}
	if rowsErr := drainRowsForExample(shortRowsResult.Payload); rowsErr != nil {
		fmt.Println("unexpected", rowsErr)
		return
	}

	longRowsResult, longErr := router.Handle(context.Background(), queryRequest{
		SQL: "SELECT value FROM widgets WHERE tenant_id = ? AND active = ?",
	})
	if longErr != nil {
		fmt.Println("unexpected", longErr)
		return
	}
	if rowsErr := drainRowsForExample(longRowsResult.Payload); rowsErr != nil {
		fmt.Println("unexpected", rowsErr)
		return
	}

	fmt.Println(replicaState.queryCallCount(), primaryState.queryCallCount())
	// Output: 1 1
}

func drainRowsForExample(rows *sql.Rows) error {
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
	}

	return rows.Err()
}
