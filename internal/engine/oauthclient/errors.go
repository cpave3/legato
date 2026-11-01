package oauthclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrInvalidGrant matches (with errors.Is) an *Error whose code is
// invalid_grant: the code or refresh token is dead, as opposed to the
// server being unreachable or failing.
var ErrInvalidGrant = errors.New("oauth: invalid_grant")

// ErrStateMismatch is returned when a redirect carries a state other than
// the one sent.
var ErrStateMismatch = errors.New("oauth: the redirect's state does not match")

// Error is an error the server answered: an OAuth error answer, or an HTTP
// status with no OAuth error in it (Code is then empty). For an error the
// authorization redirect carried, Status is 0.
type Error struct {
	Code        string
	Description string
	Status      int
	op          string
}

func (e *Error) Error() string {
	msg := "oauth: " + e.op
	if e.Status != 0 {
		msg += fmt.Sprintf(" answered %d", e.Status)
	}
	if e.Code != "" {
		msg += ": " + e.Code
	}
	if e.Description != "" {
		msg += ": " + e.Description
	}
	return msg
}

// Is makes errors.Is(err, ErrInvalidGrant) true for an invalid_grant answer.
func (e *Error) Is(target error) bool {
	return target == ErrInvalidGrant && e.Code == "invalid_grant"
}

// errorFromResponse reads an OAuth error answer. The body itself never goes
// into the error, only its error code and description.
func errorFromResponse(op string, status int, body []byte) *Error {
	var oe struct {
		Code        string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &oe)
	return &Error{Code: oe.Code, Description: oe.Description, Status: status, op: op}
}

func httpClient(c *http.Client) *http.Client {
	if c == nil {
		return http.DefaultClient
	}
	return c
}
