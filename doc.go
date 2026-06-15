// Package routery provides generic, composable routing and resiliency primitives.
//
// Declarative ingress is [RouteTable] → [Router.Dispatch], which returns [RouteOutcome]
// with payload, action, and reason code. Leaf handlers use [RouteHandler] and
// [ResultRecorder] to record Stop, Next, Ignore, and Async dispositions.
//
// Disposition semantics:
//   - Next — continue to the next route or fallback; use in RouteTable when declining without terminating.
//   - Ignore — terminal stop without payload; does not invoke fallback in RouteTable.
//   - Stop / Async — terminal stop with payload.
//   - Handler error — abort; [RouteOutcome].Action is [ActionAbort]; check err before payload.
package routery
