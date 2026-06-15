package routeryhttp_test

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"time"

	"github.com/skosovsky/routery"
	routeryhttp "github.com/skosovsky/routery/ext/http"
)

func ExampleNewRouteHandler_withRetryIf() {
	attempts := 0
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(stdhttp.StatusServiceUnavailable)
			_, _ = w.Write([]byte("retry"))
			return
		}

		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	request, _ := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)

	handler := routery.ApplyRoute(
		routeryhttp.NewRouteHandler(server.Client()),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, routeryhttp.DefaultRetryPolicy),
	)

	outcome, err := routery.InvokeRouteHandler(context.Background(), request, handler)
	if err != nil {
		fmt.Println("unexpected", err)
		return
	}
	if !outcome.HasPayload {
		fmt.Println("unexpected", "no payload")
		return
	}
	response := outcome.Payload
	defer response.Body.Close()

	fmt.Println(response.StatusCode, attempts)
	// Output: 200 2
}

func ExampleNewRouteHandler_retryExhausted() {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusServiceUnavailable)
		_, _ = w.Write([]byte("still failing"))
	}))
	defer server.Close()

	request, _ := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)

	handler := routery.ApplyRoute(
		routeryhttp.NewRouteHandler(server.Client()),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, routeryhttp.DefaultRetryPolicy),
	)

	outcome, err := routery.InvokeRouteHandler(context.Background(), request, handler)
	if outcome.HasPayload {
		fmt.Println("unexpected outcome payload")
		return
	}

	var statusErr *routeryhttp.StatusError
	if !errors.As(err, &statusErr) || statusErr.Response == nil {
		fmt.Println("unexpected error shape")
		return
	}
	defer statusErr.Response.Body.Close()

	fmt.Println(errors.As(err, &statusErr), statusErr.Response.StatusCode)
	// Output: true 503
}

func ExampleTimeout_withRetryIf() {
	attempts := 0
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(stdhttp.StatusServiceUnavailable)
			_, _ = w.Write([]byte("retry"))
			return
		}

		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	request, _ := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)

	handler := routery.ApplyRoute(
		routeryhttp.NewRouteHandler(server.Client()),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, routeryhttp.DefaultRetryPolicy),
		routeryhttp.Timeout(500*time.Millisecond),
	)

	outcome, err := routery.InvokeRouteHandler(context.Background(), request, handler)
	if err != nil {
		fmt.Println("unexpected", err)
		return
	}
	if !outcome.HasPayload {
		fmt.Println("unexpected", "no payload")
		return
	}
	response := outcome.Payload
	defer response.Body.Close()

	fmt.Println(response.StatusCode, attempts)
	// Output: 200 2
}
