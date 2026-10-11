package routeryhttp_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	routeryhttp "github.com/skosovsky/routery/ext/http"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

type countedStream struct {
	io.Reader

	closes atomic.Int32
	err    error
}

func (body *countedStream) Close() error { body.closes.Add(1); return body.err }
func stream(text string, err error) *countedStream {
	return &countedStream{Reader: strings.NewReader(text), err: err}
}
func requestFor(t *testing.T, method string, body io.Reader) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, "http://unused.invalid", body)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
func handled(response *http.Response, life *routery.Lifetime) routery.BasicRouteResult[*http.Response] {
	result := routery.BasicHandled(response)
	result.Lifetime = life
	return result
}

func constantHandler(
	result routery.BasicRouteResult[*http.Response],
	err error,
) routery.BasicRouteHandler[*http.Request, *http.Response] {
	return func(routery.RouteCall[*http.Request]) (routery.BasicRouteResult[*http.Response], error) {
		return result, err
	}
}
func requireCauses(t *testing.T, err error, causes ...error) {
	t.Helper()
	for _, cause := range causes {
		if !errors.Is(err, cause) {
			t.Fatalf("error %v missing %v", err, cause)
		}
	}
}

func TestRoundTripperPreservesRequest(t *testing.T) {
	// Arrange.
	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "value")
	body := stream("streaming POST", nil)
	request := requestFor(t, http.MethodPost, body).WithContext(ctx)
	request.Header.Set("X-Test", "original")
	request.Host = "caller.host"
	var calls int
	leaf := transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req == request || req.Method != http.MethodPost || req.Context().Value(key{}) != "value" ||
			req.Header.Get("X-Test") != "original" ||
			req.GetBody != nil {
			t.Fatal("request contract lost")
		}
		data, err := io.ReadAll(req.Body)
		if err != nil || string(data) != "streaming POST" {
			t.Fatalf("body=%q err=%v", data, err)
		}
		req.Header.Set("X-Test", "leaf")
		req.Host = "leaf.host"
		_ = req.Body.Close()
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	adapter := routeryhttp.NewRoundTripper(
		func(call routery.RouteCall[*http.Request]) (routery.BasicRouteResult[*http.Response], error) {
			response, err := leaf.RoundTrip(call.Request)
			return routery.BasicHandled(response), err
		},
	)
	// Act.
	response, err := adapter.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	_ = response.Body.Close()
	// Assert.
	if calls != 1 || body.closes.Load() != 1 || request.Body != body || request.Header.Get("X-Test") != "original" ||
		request.Host != "caller.host" ||
		request.Context() != ctx {
		t.Fatal("mutated request or wrong dispatch/ownership")
	}
}

func TestRoundTripperShortCircuitClosesRequest(t *testing.T) {
	for _, scenario := range []string{"rejected", "canceled", "nil handler", "leaf closed"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange.
			cleanupErr := errors.New("request close")
			primary := routery.ErrCircuitOpen
			body := stream("post", cleanupErr)
			request := requestFor(t, http.MethodPost, body)
			var leafCalls int
			leaf := func(call routery.RouteCall[*http.Request]) (routery.BasicRouteResult[*http.Response], error) {
				leafCalls++
				_ = call.Request.Body.Close()
				return routery.BasicIgnored[*http.Response](routery.BasicReasonNone), primary
			}
			reject := func(_ routery.BasicRouteHandler[*http.Request, *http.Response]) routery.BasicRouteHandler[*http.Request, *http.Response] {
				return constantHandler(routery.BasicIgnored[*http.Response](routery.BasicReasonNone), primary)
			}
			handler := routery.ApplyRoute(leaf, reject)
			switch scenario {
			case "canceled":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				request = request.WithContext(ctx)
				primary = context.Canceled
				handler = leaf
			case "nil handler":
				handler = nil
				primary = routery.ErrInvalidConfig
			case "leaf closed":
				handler = leaf
			}
			// Act.
			response, err := routeryhttp.NewRoundTripper(handler).RoundTrip(request)
			// Assert.
			requireCauses(t, err, primary, cleanupErr)
			wantCalls := 0
			if scenario == "leaf closed" {
				wantCalls = 1
			}
			if response != nil || leafCalls != wantCalls || body.closes.Load() != 1 || request.Body != body {
				t.Fatal("short circuit ownership")
			}
		})
	}
	// Arrange / Act / Assert: nil request cannot invoke a handler.
	_, err := routeryhttp.NewRoundTripper(nil).RoundTrip(nil)
	requireCauses(t, err, routery.ErrInvalidConfig)
}

func TestRoundTripperGetBodyClosesOriginalAndAttempt(t *testing.T) {
	// Arrange.
	requestErr, responseErr := errors.New("original close"), errors.New("response close")
	original, attempt, responseBody := stream(
		"post",
		requestErr,
	), stream(
		"post",
		errors.New("attempt close"),
	), stream(
		"ok",
		responseErr,
	)
	request := requestFor(t, http.MethodPost, original)
	request.GetBody = func() (io.ReadCloser, error) { return attempt, nil }
	var dispatches int
	leaf := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		dispatches++
		if req.Body != attempt {
			t.Fatal("wrong attempt body")
		}
		_ = req.Body.Close()
		return &http.Response{StatusCode: http.StatusOK, Body: responseBody}, nil
	})}
	// Act.
	response, err := routeryhttp.NewRoundTripper(routeryhttp.NewRouteHandler(leaf)).RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	// Assert: original body remains live on headers.
	if original.closes.Load() != 0 || responseBody.closes.Load() != 0 || attempt.closes.Load() != 1 {
		t.Fatal("premature close")
	}
	closeErr := response.Body.Close()
	repeated := response.Body.Close()
	requireCauses(t, closeErr, requestErr, responseErr)
	//nolint:errorlint // Verify repeated Close returns the same cached error object.
	if repeated != closeErr || original.closes.Load() != 1 || responseBody.closes.Load() != 1 ||
		attempt.closes.Load() != 1 ||
		dispatches != 1 {
		t.Fatal("combined ownership")
	}
}

func TestRoundTripperSuccessfulShortCircuitClosesRequest(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(strconv.FormatBool(empty), func(t *testing.T) {
			// Arrange.
			requestErr, responseErr := errors.New("request close"), errors.New("response close")
			requestBody, responseBody := stream("post", requestErr), stream("ok", nil)
			response := &http.Response{StatusCode: http.StatusOK}
			if !empty {
				response.Body = responseBody
			}
			var ownerCloses, leafCalls int
			life := routery.NewLifetime(func() error {
				ownerCloses++
				if !empty {
					_ = responseBody.Close()
				}
				return responseErr
			})
			leaf := func(routery.RouteCall[*http.Request]) (routery.BasicRouteResult[*http.Response], error) {
				leafCalls++
				return routery.BasicIgnored[*http.Response](routery.BasicReasonNone), errors.New("unexpected leaf")
			}
			middleware := func(_ routery.BasicRouteHandler[*http.Request, *http.Response]) routery.BasicRouteHandler[*http.Request, *http.Response] {
				return constantHandler(handled(response, life), nil)
			}
			// Act.
			got, err := routeryhttp.NewRoundTripper(routery.ApplyRoute(leaf, middleware)).
				RoundTrip(requestFor(t, http.MethodPost, requestBody))
			if err != nil || got.Body == nil {
				t.Fatalf("response=%v err=%v", got, err)
			}
			// Assert.
			if ownerCloses != 0 || requestBody.closes.Load() != 0 {
				t.Fatal("premature close")
			}
			closeErr := got.Body.Close()
			requireCauses(t, closeErr, requestErr, responseErr)
			//nolint:errorlint // Verify repeated Close returns the same cached error object.
			if got.Body.Close() != closeErr || ownerCloses != 1 || requestBody.closes.Load() != 1 || leafCalls != 0 {
				t.Fatal("short circuit leak")
			}
		})
	}
}

//nolint:gocognit // Table-driven ownership matrix keeps each scenario explicit.
func TestRoundTripperEmptyBodyLifetime(t *testing.T) {
	for _, scenario := range []struct {
		name, method string
		status       int
		length       int64
		valid        bool
	}{
		{"head", http.MethodHead, http.StatusOK, 10, true}, {"204", http.MethodGet, http.StatusNoContent, 0, true},
		{"zero", http.MethodGet, http.StatusOK, 0, true}, {"nonzero", http.MethodGet, http.StatusOK, 10, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, withLife := range []bool{false, true} {
				// Arrange.
				body := stream("post", nil)
				response := &http.Response{StatusCode: scenario.status, ContentLength: scenario.length}
				var closes int
				var life *routery.Lifetime
				if withLife {
					life = routery.NewLifetime(func() error { closes++; return nil })
				}
				// Act.
				got, err := routeryhttp.NewRoundTripper(constantHandler(handled(response, life), nil)).
					RoundTrip(requestFor(t, scenario.method, body))

					// Assert.
				if scenario.valid {
					if err != nil || got == nil || got.Body == nil {
						t.Fatalf("response=%v err=%v", got, err)
					}
					if closes != 0 || body.closes.Load() != 0 {
						t.Fatal("premature empty close")
					}
					_ = got.Body.Close()
					_ = got.Body.Close()
				} else {
					if got != nil {
						t.Fatal("invalid response escaped")
					}
					requireCauses(t, err, routery.ErrInvalidConfig)
				}
				want := 0
				if withLife {
					want = 1
				}
				if closes != want || body.closes.Load() != 1 {
					t.Fatal("empty body leak")
				}
			}
		})
	}
}

//nolint:gocognit // Table-driven ownership matrix keeps each scenario explicit.
func TestRoundTripperAbandonedResult(t *testing.T) {
	for _, scenario := range []string{"nil payload", "missing payload", "partial error", "invalid action"} {
		for _, withLife := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", scenario, withLife), func(t *testing.T) {
				// Arrange.
				cleanupErr, requestErr, primary := errors.New(
					"response cleanup",
				), errors.New(
					"request cleanup",
				), errors.New(
					"primary",
				)
				responseBody, requestBody := stream("response", cleanupErr), stream("post", requestErr)
				result := routery.BasicHandled(&http.Response{StatusCode: http.StatusOK, Body: responseBody})
				var cause error
				var ownerCloses int
				if withLife {
					result.Lifetime = routery.NewLifetime(func() error { ownerCloses++; return responseBody.Close() })
				}
				switch scenario {
				case "nil payload":
					result.Payload = nil
				case "missing payload":
					result.HasPayload = false
				case "partial error":
					cause = primary
				case "invalid action":
					result.Action = "invalid"
				}
				// Act.
				response, err := routeryhttp.NewRoundTripper(constantHandler(result, cause)).
					RoundTrip(requestFor(t, http.MethodPost, requestBody))

					// Assert.
				requireCauses(t, err, requestErr)
				if cause != nil {
					requireCauses(t, err, primary)
				} else {
					requireCauses(t, err, routery.ErrInvalidConfig)
				}
				wantBody := int32(1)
				if scenario == "nil payload" && !withLife {
					wantBody = 0
				} else {
					requireCauses(t, err, cleanupErr)
				}
				wantOwner := 0
				if withLife {
					wantOwner = 1
				}
				if response != nil || responseBody.closes.Load() != wantBody || ownerCloses != wantOwner ||
					requestBody.closes.Load() != 1 {
					t.Fatal("abandoned resource ownership")
				}
			})
		}
	}
}

//nolint:gocognit // Table-driven ownership matrix keeps each scenario explicit.
func TestRoundTripperStatusError(t *testing.T) {
	for _, scenario := range []string{"429", "500", "wrapped", "joined cancel", "joined cleanup", "missing response", "conflict", "shared body", "joined statuses"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange.
			cleanupErr := errors.New("cleanup")
			body, other := stream("status", cleanupErr), stream("other", cleanupErr)
			response := &http.Response{StatusCode: http.StatusInternalServerError, Body: body}
			if scenario == "429" {
				response.StatusCode = http.StatusTooManyRequests
			}
			status := &routeryhttp.StatusError{Code: response.StatusCode, Response: response}
			result := routery.AbortResult[routery.BasicKind, routery.BasicReason, *http.Response]()
			result.Lifetime = routery.NewLifetime(body.Close)
			var cause error = status
			valid := true
			switch scenario {
			case "wrapped":
				cause = fmt.Errorf("wrapped: %w", status)
			case "joined cancel":
				cause = errors.Join(status, context.Canceled)
				valid = false
			case "joined cleanup":
				cause = errors.Join(status, cleanupErr)
				valid = false
			case "missing response":
				status.Response = nil
				valid = false
			case "conflict":
				result.Payload = &http.Response{Body: other}
				valid = false
				result.Lifetime = routery.NewLifetime(other.Close)
			case "shared body":
				result.Payload = &http.Response{Body: body}
				valid = false
			case "joined statuses":
				cause = errors.Join(
					status,
					&routeryhttp.StatusError{Response: &http.Response{Body: other}, Code: http.StatusTooManyRequests},
				)
				valid = false
			}
			// Act: exercise the outer net/http.Client as a consumer.
			client := &http.Client{Transport: routeryhttp.NewRoundTripper(constantHandler(result, cause))}
			got, err := client.Do(requestFor(t, http.MethodGet, nil))
			// Assert.
			if valid {
				if err != nil || got == nil || got.StatusCode != response.StatusCode || body.closes.Load() != 0 {
					t.Fatalf("status mapping response=%v err=%v", got, err)
				}
				requireCauses(t, got.Body.Close(), cleanupErr)
				requireCauses(t, got.Body.Close(), cleanupErr)
			} else {
				if got != nil {
					t.Fatal("response and error returned together")
				}
				requireCauses(t, err, status, cleanupErr)
				if scenario == "joined cancel" {
					requireCauses(t, err, context.Canceled)
				}
			}
			if body.closes.Load() != 1 {
				t.Fatal("status resource leak or double close")
			}
			if (scenario == "conflict" || scenario == "joined statuses") && other.closes.Load() != 1 {
				t.Fatal("conflicting resource leak")
			}
		})
	}
}

func TestRoundTripperNoImplicitRetry(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			// Arrange.
			body := stream("post", nil)
			request := requestFor(t, http.MethodPost, body)
			var calls int
			leaf := transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				_ = req.Body.Close()
				if fail {
					return nil, io.ErrUnexpectedEOF
				}
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody}, nil
			})
			handler := func(call routery.RouteCall[*http.Request]) (routery.BasicRouteResult[*http.Response], error) {
				resp, err := leaf.RoundTrip(call.Request)
				return routery.BasicHandled(resp), err
			}
			// Act.
			response, err := routeryhttp.NewRoundTripper(handler).RoundTrip(request)
			if fail {
				requireCauses(t, err, io.ErrUnexpectedEOF)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				var wg sync.WaitGroup
				for range 10 {
					wg.Go(func() { _ = response.Body.Close() })
				}
				wg.Wait()
			}
			// Assert.
			if calls != 1 || body.closes.Load() != 1 {
				t.Fatal("implicit retry or double cleanup")
			}
		})
	}
}

func await(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(5 * time.Second):
		t.Fatal("channel timed out")
	}
}

func TestRoundTripperRetainsLifetimeUntilBodyClose(t *testing.T) {
	// Arrange: real HTTP headers precede body; child context owns the resource.
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseBody := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
			_, _ = io.WriteString(w, "delayed")
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(func() { releaseBody(); server.Close() })
	request := requestFor(t, http.MethodGet, nil)
	request.URL, _ = request.URL.Parse(server.URL)
	cleanupErr := errors.New("cleanup")
	var child context.Context
	var closes atomic.Int32
	leaf := routeryhttp.NewRouteHandler(server.Client())
	handler := func(call routery.RouteCall[*http.Request]) (routery.BasicRouteResult[*http.Response], error) {
		var cancel context.CancelFunc
		//nolint:fatcontext // This fixture deliberately creates a handler-owned child context.
		child, cancel = context.WithCancel(call.Context)
		call.Context = child
		result, err := leaf(call)
		owner := result.Lifetime
		result.Lifetime = routery.NewLifetime(
			func() error { closes.Add(1); cancel(); return errors.Join(owner.Close(), cleanupErr) },
		)
		return result, err
	}
	// Act.
	response, err := routeryhttp.NewRoundTripper(handler).RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	// Assert.
	if child.Err() != nil || closes.Load() != 0 {
		t.Fatal("context canceled at headers")
	}
	releaseBody()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "delayed" {
		t.Fatalf("body=%q err=%v", data, err)
	}
	closeErr := response.Body.Close()
	requireCauses(t, closeErr, cleanupErr)
	//nolint:errorlint // Verify repeated Close returns the same cached error object.
	if response.Body.Close() != closeErr || closes.Load() != 1 || child.Err() != context.Canceled {
		t.Fatal("lifetime cleanup")
	}
}

//nolint:gocognit // Table-driven ownership matrix keeps each scenario explicit.
func TestRoundTripperCancellation(t *testing.T) {
	for _, afterHeaders := range []bool{false, true} {
		t.Run(strconv.FormatBool(afterHeaders), func(t *testing.T) {
			// Arrange.
			entered := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if afterHeaders {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				close(entered)
				<-req.Context().Done()
			}))
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(func() { cancel(); server.Close() })
			request := requestFor(t, http.MethodGet, nil).WithContext(ctx)
			request.URL, _ = request.URL.Parse(server.URL)
			var closes atomic.Int32
			leaf := routeryhttp.NewRouteHandler(server.Client())
			handler := func(call routery.RouteCall[*http.Request]) (routery.BasicRouteResult[*http.Response], error) {
				result, err := leaf(call)
				if result.Lifetime != nil {
					owner := result.Lifetime
					result.Lifetime = routery.NewLifetime(func() error { closes.Add(1); return owner.Close() })
				}
				return result, err
			}
			adapter := routeryhttp.NewRoundTripper(handler)
			// Act / Assert.
			if !afterHeaders {
				done := make(chan struct{})
				var response *http.Response
				var err error
				go func() { response, err = adapter.RoundTrip(request); close(done) }()
				await(t, entered)
				cancel()
				await(t, done)
				requireCauses(t, err, context.Canceled)
				if response != nil {
					t.Fatal("response on cancellation")
				}
			} else {
				response, err := adapter.RoundTrip(request)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = response.Body.Close() })
				await(t, entered)
				reading, done := make(chan struct{}), make(chan struct{})
				var readErr error
				go func() { close(reading); _, readErr = io.ReadAll(response.Body); close(done) }()
				await(t, reading)
				cancel()
				await(t, done)
				requireCauses(t, readErr, context.Canceled)
				_ = response.Body.Close()
				_ = response.Body.Close()
				if closes.Load() != 1 {
					t.Fatal("canceled body leak")
				}
			}
		})
	}
}

func TestRoundTripperConsumerFixture(t *testing.T) {
	// Arrange: a second consumer uses a standard client, GET 500 and delayed body.
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.(http.Flusher).Flush()
		select {
		case <-release:
			_, _ = io.WriteString(w, "consumer payload")
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(func() { unblock(); server.Close() })
	leafClient := server.Client()
	handler := routery.ApplyRoute(routeryhttp.NewRouteHandler(leafClient), routeryhttp.Timeout(time.Minute))
	outerClient := &http.Client{Transport: routeryhttp.NewRoundTripper(handler)}
	request := requestFor(t, http.MethodGet, nil)
	request.URL, _ = request.URL.Parse(server.URL)
	// Act.
	response, err := outerClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	unblock()
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	repeatErr := response.Body.Close()
	// Assert.
	if response.StatusCode != http.StatusInternalServerError || readErr != nil || string(data) != "consumer payload" ||
		closeErr != nil ||
		repeatErr != nil ||
		calls.Load() != 1 ||
		outerClient.Transport == leafClient.Transport {
		t.Fatalf(
			"consumer regression: status=%d body=%q read=%v close=%v calls=%d",
			response.StatusCode,
			data,
			readErr,
			closeErr,
			calls.Load(),
		)
	}
}

//nolint:gocognit,nestif // Table-driven ownership matrix keeps each scenario explicit.
func TestRoundTripperStatusWithoutLifetime(t *testing.T) {
	for _, scenario := range []string{"status", "joined", "invalid body", "same payload", "conflict", "nil canonical body"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange.
			cleanupErr := errors.New("response close")
			body, other := stream("status", cleanupErr), stream("other", cleanupErr)
			response := &http.Response{StatusCode: http.StatusInternalServerError, Body: body}
			status := &routeryhttp.StatusError{Response: response, Code: response.StatusCode}
			result := routery.AbortResult[routery.BasicKind, routery.BasicReason, *http.Response]()
			var cause error = status
			valid := scenario == "status" || scenario == "same payload"
			switch scenario {
			case "joined":
				cause = errors.Join(status, context.Canceled)
			case "invalid body":
				response.Body = nil
				response.ContentLength = 5
			case "same payload":
				result.Payload = response
				result.HasPayload = true
			case "conflict":
				result.Payload = &http.Response{Body: other}
			case "nil canonical body":
				result.Payload = &http.Response{}
			}
			// Act.
			got, err := routeryhttp.NewRoundTripper(constantHandler(result, cause)).
				RoundTrip(requestFor(t, http.MethodGet, nil))

			// Assert.
			if valid {
				if err != nil {
					t.Fatal(err)
				}
				requireCauses(t, got.Body.Close(), cleanupErr)
				requireCauses(t, got.Body.Close(), cleanupErr)
			} else {
				if got != nil {
					t.Fatal("invalid response escaped")
				}
				if scenario == "invalid body" {
					requireCauses(t, err, routery.ErrInvalidConfig)
				} else {
					requireCauses(t, err, status, cleanupErr)
				}
			}
			want := int32(1)
			if scenario == "invalid body" {
				want = 0
			}
			if body.closes.Load() != want {
				t.Fatal("status body leaked/double closed")
			}
			if scenario == "conflict" && other.closes.Load() != 1 {
				t.Fatal("payload leaked")
			}
		})
	}
}
