package routerysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/skosovsky/routery"
)

func ExampleNewDBQueryRouteHandler() {
	db, state, err := openTestDBWithName(uniqueDriverName("example-query"), testDriverConfig{})
	if err != nil {
		fmt.Println("unexpected", err)
		return
	}
	defer db.Close()

	type queryRequest struct {
		ID int
	}

	handler := NewDBQueryRouteHandler(
		db,
		func(_ context.Context, req queryRequest) (string, []any, error) {
			return "SELECT value FROM widgets WHERE id = ?", []any{req.ID}, nil
		},
	)

	outcome, executeErr := routery.InvokeRouteHandler(context.Background(), queryRequest{ID: 1}, handler)
	if executeErr != nil {
		fmt.Println("unexpected", executeErr)
		return
	}
	if !outcome.HasPayload {
		fmt.Println("unexpected", "no payload")
		return
	}
	if rowsErr := drainRowsForExample(outcome.Payload); rowsErr != nil {
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

	primaryRouteHandler := NewDBQueryRouteHandler(
		primaryDB,
		func(_ context.Context, req queryRequest) (string, []any, error) {
			return req.SQL, nil, nil
		},
	)
	replicaRouteHandler := NewDBQueryRouteHandler(
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
		replicaRouteHandler,
		primaryRouteHandler,
	)

	shortOutcome, shortErr := routery.InvokeRouteHandler(context.Background(), queryRequest{
		SQL: "SELECT value FROM widgets",
	}, router)
	if shortErr != nil {
		fmt.Println("unexpected", shortErr)
		return
	}
	if !shortOutcome.HasPayload {
		fmt.Println("unexpected", "no payload")
		return
	}
	if rowsErr := drainRowsForExample(shortOutcome.Payload); rowsErr != nil {
		fmt.Println("unexpected", rowsErr)
		return
	}

	longOutcome, longErr := routery.InvokeRouteHandler(context.Background(), queryRequest{
		SQL: "SELECT value FROM widgets WHERE tenant_id = ? AND active = ?",
	}, router)
	if longErr != nil {
		fmt.Println("unexpected", longErr)
		return
	}
	if !longOutcome.HasPayload {
		fmt.Println("unexpected", "no payload")
		return
	}
	if rowsErr := drainRowsForExample(longOutcome.Payload); rowsErr != nil {
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
