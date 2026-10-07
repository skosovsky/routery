// Package routeryprompty provides opt-in ownership of lazy response streams.
package routeryprompty

import (
	"errors"

	"github.com/skosovsky/prompty"

	"github.com/skosovsky/routery/stream"
)

// New transfers exclusive consumption of source to a routing lifetime owner.
// Call owner.Cancel from consumer callbacks and owner.Close outside consumption.
// source must be created and unused; do not separately consume or close it after transfer.
func New(source *prompty.Stream) (*stream.Owner[*prompty.ResponseChunk], error) {
	if source == nil {
		return nil, errors.New("routery/ext/prompty: nil stream")
	}
	if source.Status().State != prompty.StreamCreated {
		return nil, prompty.ErrStreamConsumed
	}
	// A created source has no dispatched operation or allocated transport to discard.
	return stream.New(source.Events(), source.Close, func() error { return nil })
}
