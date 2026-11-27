# 3. The TUI uses engine values, not engine behaviour

Status: accepted, 2026-11-27 (Cameron)

## Context

CLAUDE.md said the TUI "imports service/ via interfaces, never engine/".
Nothing enforced that rule, and 14 TUI imports broke it. A CodeEval audit
(task i95qqzwb) found that nearly every one of them names a value type, a
constant or a pure function: store records, workspace views, event types
and payloads, diff lines, the macro model, PR status, analytics time
ranges. Two of the imports are behaviour: holding and calling `*events.Bus`
(subscribe and publish) and `ipc.Send` (answering a swarm conductor).

## Decision

The TUI may name engine value types and pure helpers that have been
audited. Engine behaviour reaches it only through service interfaces.

- `service.EventStream` (Subscribe, Publish) stands for the event bus.
  `*events.Bus` implements it, and `NewApp` takes the interface.
- `service.SendPlanVerdict` answers a conductor. The TUI does not import
  `ipc`.
- `internal/tui/architecture_test.go` checks each symbol, not each
  package. Every package-qualified engine name in non-test TUI code must
  be on its list (`engineValues`). For `events`, the list means `Event`,
  `EventType`, the `Event…` constants and the `…Payload` types, never `Bus`
  or `New`. An engine package not on the list cannot be imported at all.

## Limits

The test sees package-qualified names only. It does not see a method
called on a value of an allowed type, so a type joins the list only after
someone has checked that none of its methods does I/O or shares state. As
of this decision no allowed type has methods. `analytics.Today`,
`ThisWeek` and `ThisMonth` read the clock and do nothing else.
