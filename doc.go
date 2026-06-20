// Package routery provides generic, composable routing and resiliency primitives.
//
// Declarative ingress is [RouteTable] → [Router.Dispatch], which returns [RouteResult]
// with engine action, caller-provided terminal kind, caller-provided reason, payload,
// and route metadata. Leaf handlers receive [RouteCall] and return [RouteResult] directly.
//
// Disposition semantics:
//   - Next — continue to the next route or fallback; use in RouteTable when declining without terminating.
//   - Ignored — terminal stop without payload; does not invoke fallback in RouteTable.
//   - Stop / Async — terminal stop with payload.
//   - Handler error — abort; [RouteResult].Action is [ActionAbort]; check err before payload.
package routery
