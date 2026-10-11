package routeryhttp

import (
	"errors"
	"io"
	stdhttp "net/http"
	"reflect"
	"slices"

	"github.com/skosovsky/routery"
)

// NewRoundTripper adapts a handler to net/http with one invocation per RoundTrip.
// It neither buffers bodies nor prepares requests, retries, or classifies failures.
// When composing with NewRouteHandler, call PrepareRequest explicitly if needed.
// A sole StatusError (possibly wrapped with Unwrap() error) becomes an HTTP response;
// joined errors remain failures. The caller must close every returned response body.
// Handlers must give each result one canonical Lifetime owning its response.
func NewRoundTripper(
	handler routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response],
) stdhttp.RoundTripper {
	return &handlerTransport{handler: handler}
}

type handlerTransport struct {
	handler routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response]
}

func (transport *handlerTransport) RoundTrip(request *stdhttp.Request) (*stdhttp.Response, error) {
	if request == nil {
		return nil, configError("request is nil")
	}
	cloned := request.Clone(request.Context())
	var requestLife *routery.Lifetime
	if request.Body != nil {
		requestLife = routery.NewLifetime(request.Body.Close)
		cloned.Body = &ownedBody{ReadCloser: request.Body, life: requestLife}
	}
	result, err := routery.InvokeRouteHandler(request.Context(), cloned, transport.handler)
	handlerErr := err
	response := result.Payload
	status := soleStatusError(err)
	if status != nil && status.Response != nil && (response == nil || response == status.Response) {
		response, err = status.Response, nil
	} else if err == nil && (!result.HasPayload || response == nil) {
		err = configError("handler returned no response")
	}
	if err == nil && response.Body == nil && response.ContentLength > 0 && request.Method != stdhttp.MethodHead {
		err = configError("response has positive content length but no body")
	}
	if err != nil {
		return nil, errors.Join(err, closeAbandonedHTTPResult(result, handlerErr), requestLife.Close())
	}
	owner := result.Lifetime
	if owner == nil && response.Body != nil {
		owner = routery.NewLifetime(response.Body.Close)
	}
	reader := response.Body
	if reader == nil {
		reader = stdhttp.NoBody
	}
	// Keep both owners alive while upload and response reading can still be active.
	combined := routery.NewLifetime(func() error { return errors.Join(owner.Close(), requestLife.Close()) })
	response.Body = &ownedBody{ReadCloser: reader, life: combined}
	return response, nil
}

// soleStatusError deliberately excludes multi-error Unwrap, including [errors.Join].
//
//nolint:errorlint // Exact error-tree traversal must reject joined causes rather than search with errors.As.
func soleStatusError(err error) *StatusError {
	for err != nil {
		if status, ok := err.(*StatusError); ok {
			return status
		}
		wrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil
		}
		err = wrapper.Unwrap()
	}
	return nil
}

// closeAbandonedHTTPResult never drains a body. A lifetime owns the payload, or
// the first status response when there is no payload. Conflicting status responses
// are separate resources; deduplicate only actual shared body identity.
//
//nolint:bodyclose // All discovered bodies are closed here or by the canonical Lifetime.
func closeAbandonedHTTPResult(result routery.BasicRouteResult[*stdhttp.Response], err error) error {
	responses := []*stdhttp.Response{result.Payload}
	responses = appendStatusResponses(responses, err)
	var cleanupErr error
	if result.Lifetime != nil {
		cleanupErr = result.Lifetime.Close()
	}
	var seen []io.ReadCloser
	var seenResponses []*stdhttp.Response
	canonical := true
	for _, response := range responses {
		if response == nil {
			continue
		}
		body := response.Body
		duplicate := slices.Contains(seenResponses, response)
		seenResponses = append(seenResponses, response)
		for _, previous := range seen {
			if body != nil && reflect.ValueOf(body).Comparable() && body == previous {
				duplicate = true
				break
			}
		}
		if body != nil {
			seen = append(seen, body)
			if !duplicate && (!canonical || result.Lifetime == nil) {
				cleanupErr = errors.Join(cleanupErr, body.Close())
			}
		}
		canonical = false
	}
	return cleanupErr
}

//nolint:bodyclose,errorlint // Traverse all error branches; cleanup remains in closeAbandonedHTTPResult.
func appendStatusResponses(responses []*stdhttp.Response, err error) []*stdhttp.Response {
	if status, ok := err.(*StatusError); ok && status != nil {
		return append(responses, status.Response)
	}
	switch wrapper := err.(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range wrapper.Unwrap() {
			responses = appendStatusResponses(responses, cause)
		}
	case interface{ Unwrap() error }:
		responses = appendStatusResponses(responses, wrapper.Unwrap())
	}
	return responses
}
