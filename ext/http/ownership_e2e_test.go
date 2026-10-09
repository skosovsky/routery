//go:build e2e

package routeryhttp

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/skosovsky/routery"
)

func TestE2EFirstSuccessfulPayloadKeepsWinnerBodyAlive(t *testing.T) {
	for _, branches := range []int{1, 2} {
		// Arrange.
		release := make(chan struct{})
		server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			w.WriteHeader(stdhttp.StatusOK)
			w.(stdhttp.Flusher).Flush()
			<-release
			_, _ = io.WriteString(w, "payload")
		}))
		request, _ := stdhttp.NewRequestWithContext(t.Context(), stdhttp.MethodGet, server.URL, nil)
		leaf := NewRouteHandler(server.Client())
		loserCancelled := make(chan struct{})
		handlers := []routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response]{leaf}
		if branches == 2 {
			handlers = append(
				handlers,
				func(call routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
					<-call.Context.Done()
					close(loserCancelled)
					return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response](), call.Context.Err()
				},
			)
		}
		// Act.
		result, err := routery.InvokeRouteHandler(t.Context(), request, routery.FirstSuccessfulPayload(handlers...))
		close(release)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(result.Payload.Body)
		// Assert.
		if readErr != nil || string(body) != "payload" {
			t.Fatalf("body=%q err=%v", body, readErr)
		}
		result.Payload.Body.Close()
		result.Lifetime.Close()
		if branches == 2 {
			<-loserCancelled
		}
		server.Close()
	}
}

func TestE2EPreparedFanoutUsesIndependentCompleteBodies(t *testing.T) {
	// Arrange.
	const branches = 4
	source := &auditReadCloser{Reader: strings.NewReader("payload")}
	request := mustNewRequest(t, stdhttp.MethodPut, nil)
	request.Body = source
	prepared, err := PrepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	var bodies sync.WaitGroup
	bodies.Add(branches)
	var sent, closed atomic.Int32
	closeEvents := make(chan struct{}, branches)
	client := &stdhttp.Client{Transport: roundTripperFunc(func(req *stdhttp.Request) (*stdhttp.Response, error) {
		data, readErr := io.ReadAll(req.Body)
		req.Body.Close()
		if readErr != nil || string(data) != "payload" {
			t.Errorf("body=%q err=%v", data, readErr)
		}
		sent.Add(1)
		bodies.Done()
		bodies.Wait()
		return &stdhttp.Response{
			StatusCode: stdhttp.StatusOK,
			Body:       &countedBody{Reader: strings.NewReader("ok"), count: &closed, events: closeEvents},
		}, nil
	})}
	leaf := NewRouteHandler(client)
	// Act.
	result, err := routery.InvokeRouteHandler(
		t.Context(),
		prepared,
		routery.FirstSuccessfulPayload(leaf, leaf, leaf, leaf),
	)
	if err != nil {
		t.Fatal(err)
	}
	result.Payload.Body.Close()
	// Assert.
	if source.closes.Load() != 1 || sent.Load() != branches || request.GetBody != nil {
		t.Fatal("preparation/fanout ownership")
	}
	// Late loser responses are drained asynchronously; each reports its close.
	for range branches {
		<-closeEvents
	}
	if closed.Load() != branches {
		t.Fatal("resources were not closed")
	}
}

type countedBody struct {
	io.Reader

	count  *atomic.Int32
	events chan<- struct{}
}

func TestE2EExplicitVerifiedReplayDeduplicatesCommittedPost(t *testing.T) {
	// Arrange.
	var calls, effects atomic.Int32
	var once sync.Once
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		once.Do(func() { effects.Add(1) })
		if calls.Add(1) == 1 {
			w.WriteHeader(stdhttp.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(stdhttp.StatusOK)
	}))
	defer server.Close()
	request, _ := stdhttp.NewRequestWithContext(t.Context(), stdhttp.MethodPost, server.URL, strings.NewReader("write"))
	explicit := RetryPolicy(
		func(context.Context, *stdhttp.Request, error) ReplaySafety { return VerifiedDeduplication },
	)
	handler := routery.ApplyRoute(
		NewRouteHandler(server.Client()),
		routery.RetryIf[*stdhttp.Request, routery.BasicKind, routery.BasicReason, *stdhttp.Response](2, 0, explicit),
	)
	// Act.
	result, err := routery.InvokeRouteHandler(t.Context(), request, handler)
	// Assert.
	if err != nil || calls.Load() != 2 || effects.Load() != 1 {
		t.Fatalf("calls=%d effects=%d err=%v", calls.Load(), effects.Load(), err)
	}
	result.Payload.Body.Close()
	unprepared := mustNewRequest(t, stdhttp.MethodPost, io.NopCloser(strings.NewReader("write")))
	defer unprepared.Body.Close()
	if explicit(t.Context(), unprepared, &StatusError{Code: stdhttp.StatusServiceUnavailable}) {
		t.Fatal("unprepared body retried")
	}
}

func (body *countedBody) Close() error { body.count.Add(1); body.events <- struct{}{}; return nil }

func TestE2EFirstSuccessfulPayloadCleansLateResultsAndParentCancel(t *testing.T) {
	// Arrange.
	ctx, cancel := context.WithCancel(t.Context())
	late := make(chan struct{})
	lateClosed := make(chan struct{})
	loserStarted := make(chan struct{})
	winner := func(routery.RouteCall[int]) (routery.BasicRouteResult[int], error) {
		<-loserStarted
		result := routery.BasicHandled(1)
		result.Lifetime = routery.NewLifetime(nil)
		return result, nil
	}
	loser := func(routery.RouteCall[int]) (routery.BasicRouteResult[int], error) {
		close(loserStarted)
		<-late
		result := routery.BasicHandled(2)
		result.Lifetime = routery.NewLifetime(func() error { close(lateClosed); return nil })
		return result, nil
	}
	// Act.
	result, err := routery.InvokeRouteHandler(ctx, 0, routery.FirstSuccessfulPayload(winner, loser))
	close(late)
	<-lateClosed
	cancel()
	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	result.Lifetime.Close()
	_, err = routery.InvokeRouteHandler(ctx, 0, routery.FirstSuccessfulPayload(winner))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation=%v", err)
	}
}
