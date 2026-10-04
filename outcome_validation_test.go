package routery

import (
	"errors"
	"testing"
)

func TestValidateRouteResultPreservesCanonicalOwnership(t *testing.T) {
	for _, action := range []RouteAction{ActionNext, ActionStop, ActionAbort, "unknown"} {
		for _, callbackError := range []bool{false, true} {
			t.Run(string(action)+map[bool]string{false: "/nil", true: "/error"}[callbackError], func(t *testing.T) {
				checkCanonicalRouteValidation(t, action, callbackError)
			})
		}
	}
}

func checkCanonicalRouteValidation(t *testing.T, action RouteAction, callbackError bool) {
	t.Helper()
	// Arrange.
	failure := errors.New("callback failure")
	closes := 0
	life := NewLifetime(func() error { closes++; return nil })
	t.Cleanup(func() { _ = life.Close() })
	result := BasicHandled(42)
	result.Action, result.Lifetime, result.Match.RouteID = action, life, "owned-partial"
	var suppliedError error
	if callbackError {
		suppliedError = failure
	}
	// Act.
	validated, err := ValidateRouteResult(result, suppliedError)
	// Assert.
	if validated.Lifetime != life || closes != 0 || !validated.HasPayload || validated.Payload != 42 ||
		validated.Kind != result.Kind || validated.Reason != result.Reason || validated.Match.RouteID != "owned-partial" {
		t.Fatal("validation lost or prematurely closed canonical owned partial")
	}
	wantAction := action
	if callbackError || action == ActionAbort || action == "unknown" {
		wantAction = ActionAbort
	}
	if validated.Action != wantAction {
		t.Fatal("invalid canonical action")
	}
	switch {
	case callbackError:
		if !errors.Is(err, failure) {
			t.Fatal("callback error identity lost")
		}
	case action == ActionAbort || action == "unknown":
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatal("malformed result was not rejected")
		}
	case err != nil:
		t.Fatal(err)
	}
}
