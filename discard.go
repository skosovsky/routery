package routery

import "errors"

// discardResult closes a result only after the final decision to discard it.
// On cleanup failure the caller receives the canonical partial result and owner.
func discardResult[Kind comparable, Reason comparable, Payload any](
	result RouteResult[Kind, Reason, Payload], cause error,
) (RouteResult[Kind, Reason, Payload], error) {
	if err := result.Lifetime.Close(); err != nil {
		result.Action = ActionAbort
		return result, errors.Join(cause, err)
	}
	return result, nil
}
