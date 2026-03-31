package routeryhttp_test

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"

	"github.com/skosovsky/routery"
	routeryhttp "github.com/skosovsky/routery/ext/http"
)

func ExampleNewExecutor_withRetryIf() {
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

	executor := routery.Apply(
		routeryhttp.NewExecutor(server.Client()),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, routeryhttp.DefaultRetryPolicy),
	)

	response, err := executor.Execute(context.Background(), request)
	if err != nil {
		fmt.Println("unexpected", err)
		return
	}
	defer response.Body.Close()

	fmt.Println(response.StatusCode, attempts)
	// Output: 200 2
}

func ExampleNewExecutor_retryExhausted() {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusServiceUnavailable)
		_, _ = w.Write([]byte("still failing"))
	}))
	defer server.Close()

	request, _ := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)

	executor := routery.Apply(
		routeryhttp.NewExecutor(server.Client()),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, routeryhttp.DefaultRetryPolicy),
	)

	response, err := executor.Execute(context.Background(), request)
	if response == nil {
		fmt.Println("unexpected nil response")
		return
	}
	defer response.Body.Close()

	var statusErr *routeryhttp.StatusError
	fmt.Println(errors.As(err, &statusErr), response.StatusCode)
	// Output: true 503
}
