package client

import "fmt"

// ErrorKind classifies a non-2xx response from the server.
type ErrorKind int

const (
	ErrorGeneric      ErrorKind = iota
	ErrorNotFound               // 404
	ErrorConflict               // 409
	ErrorUnavailable            // 503
	ErrorIncompatible           // 422 or version mismatch
	ErrorUsage                  // 400
)

// APIError is returned for non-2xx responses.
type APIError struct {
	Kind    ErrorKind
	Status  int
	Message string
	Detail  string
	Details map[string]any
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Detail)
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}
