package routery

import (
	"errors"
	"testing"
)

type testKind string

const (
	testKindHandled testKind = "handled"
	testKindIgnored testKind = "ignored"
	testKindAsync   testKind = "async"
)

type testReason string

const (
	testReasonNone       testReason = ""
	testReasonHandled    testReason = "handled"
	testReasonDelegate   testReason = "delegate"
	testReasonNoMatch    testReason = "no_match"
	testReasonClassified testReason = "classified"
)

func TestRouteResultUsesTypedKindAndReason(t *testing.T) {
	// Arrange.
	payload := "ok"

	// Act.
	result := Handled(testKindHandled, testReasonHandled, payload)

	// Assert.
	if result.Action != ActionStop {
		t.Fatalf("Action = %q, want %q", result.Action, ActionStop)
	}
	if result.Kind != testKindHandled {
		t.Fatalf("Kind = %q, want %q", result.Kind, testKindHandled)
	}
	if result.Reason != testReasonHandled {
		t.Fatalf("Reason = %q, want %q", result.Reason, testReasonHandled)
	}
	if !result.HasPayload || result.Payload != payload {
		t.Fatalf("payload = (%v, %v), want (%v, true)", result.Payload, result.HasPayload, payload)
	}
}

func TestInvokeRouteHandlerReturnsAbortOnError(t *testing.T) {
	// Arrange.
	wantErr := errors.New("boom")
	handler := func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		return Handled(testKindHandled, testReasonHandled, "ignored"), wantErr
	}

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 1, handler)

	// Assert.
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if result.Action != ActionAbort {
		t.Fatalf("Action = %q, want %q", result.Action, ActionAbort)
	}
}

func TestInvokeRouteHandlerRejectsUnexpectedAction(t *testing.T) {
	// Arrange.
	handler := func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		return RouteResult[testKind, testReason, string]{
			Action:     RouteAction("unknown"),
			Kind:       testKindHandled,
			Reason:     testReasonHandled,
			Payload:    "",
			HasPayload: false,
			Match:      zeroRouteMatch(),
		}, nil
	}

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 1, handler)

	// Assert.
	if err == nil {
		t.Fatal("err = nil, want config error")
	}
	if result.Action != ActionAbort {
		t.Fatalf("Action = %q, want %q", result.Action, ActionAbort)
	}
}
