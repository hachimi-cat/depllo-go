package depllo

import "fmt"

// Error is the single error type returned by every call in this package. Code carries
// the API envelope's error.code (UPPER_SNAKE_CASE, e.g. NOT_FOUND, VALIDATION_ERROR,
// AUTH_REQUIRED) or an SDK-side code (AUTH_REQUIRED, NETWORK_ERROR, INVALID_RESPONSE).
// Status is the HTTP status (0 for transport-level failures).
type Error struct {
	Status    int
	Code      string
	Message   string
	RequestID string
}

func (e *Error) Error() string {
	return fmt.Sprintf("depllo: %s: %s", e.Code, e.Message)
}
