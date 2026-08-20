package stackit

import (
	"errors"
	"strings"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
)

// ErrDeleteInstanceForbidden is returned when a temporary instance could not be deleted due to permission constraints (HTTP 403).
var ErrDeleteInstanceForbidden = errors.New("instance could not be deleted due to permissions (403 Forbidden)")

// IsDeleteForbidden checks strictly whether an error is ErrDeleteInstanceForbidden or an OpenAPI HTTP 403 Forbidden error.
// Brittle string-matching fallbacks are explicitly excluded.
func IsDeleteForbidden(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDeleteInstanceForbidden) {
		return true
	}
	var oapiErr *oapierror.GenericOpenAPIError
	if errors.As(err, &oapiErr) && oapiErr.StatusCode == 403 {
		return true
	}
	return false
}

// IsNotFound checks whether an error indicates a 404 Not Found response.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var oapiErr *oapierror.GenericOpenAPIError
	if errors.As(err, &oapiErr) && oapiErr.StatusCode == 404 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}
