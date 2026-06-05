package integrations

import "errors"

// ErrTicketNotFound is the source-agnostic sentinel a TicketSource reports (via
// errors.Is) when a requested ticket does not exist. Forge-neutral callers — in
// particular the `nebula new` command — match against this sentinel so they can
// distinguish "ticket doesn't exist" from other failures without importing any
// adapter-specific error type. Adapters expose it by implementing an Is method
// on their concrete not-found error that returns true for this value.
var ErrTicketNotFound = errors.New("ticket not found")
