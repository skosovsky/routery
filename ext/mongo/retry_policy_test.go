package routerymongo

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
)

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()

	type row struct {
		name string
		ctx  context.Context
		req  any
		err  error
		want bool
	}

	dup := mongo.WriteException{
		WriteErrors: []mongo.WriteError{{Code: 11000, Message: "dup"}},
	}

	tests := []row{
		{"nil", context.Background(), nil, nil, false},
		{"canceled", context.Background(), nil, context.Canceled, false},
		{"deadline", context.Background(), nil, context.DeadlineExceeded, false},
		{"no_docs", context.Background(), nil, mongo.ErrNoDocuments, false},
		{"dup", context.Background(), nil, dup, false},
		{"disconnected", context.Background(), nil, mongo.ErrClientDisconnected, true},
		{"network_label", context.Background(), nil, mongo.CommandError{Labels: []string{"NetworkError"}}, true},
		{"timeout_label", context.Background(), nil, mongo.CommandError{Labels: []string{"NetworkTimeoutError"}}, true},
		{"write_auth", context.Background(), nil, mongo.WriteException{
			WriteErrors: []mongo.WriteError{{Code: 13, Message: "auth"}},
		}, false},
		{"write_concern_auth", context.Background(), nil, mongo.WriteException{
			WriteConcernError: &mongo.WriteConcernError{Code: 13, Message: "auth"},
		}, false},
		{"write_concern_ok_retry", context.Background(), nil, mongo.WriteException{
			WriteErrors: []mongo.WriteError{{Code: 2, Message: "bad value"}},
		}, false},
		{"generic", context.Background(), nil, errors.New("x"), false},
		{"tx_req", context.Background(), txFlag(true), mongo.ErrClientDisconnected, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DefaultRetryPolicy[any](tc.ctx, tc.req, tc.err)
			if got != tc.want {
				t.Fatalf("got %v want %v err=%v", got, tc.want, tc.err)
			}
		})
	}
}

type txFlag bool

func (f txFlag) MongoInTransaction() bool { return bool(f) }

func FuzzDefaultRetryPolicyNoPanics(f *testing.F) {
	f.Add(int32(0), "m")
	f.Fuzz(func(t *testing.T, code int32, msg string) {
		t.Helper()
		err := mongo.CommandError{Code: code, Message: msg}
		_ = DefaultRetryPolicy[any](context.Background(), nil, err)
	})
}
