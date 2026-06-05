package sensors

import "errors"

// ErrTicketNotFound is the source-agnostic sentinel a Sensor adapter reports
// (via errors.Is) when a specifically requested item does not exist. Adapters
// expose it by implementing an Is method on their concrete not-found error that
// returns true for this value. It is retained from the ticket-ingest phase so
// adapter error classification stays uniform; the poll-driven flow rarely hits
// it, but a future on-demand "seed from this reference" surface can match it.
var ErrTicketNotFound = errors.New("ticket not found")
