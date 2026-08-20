package stackit

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
)

// customFake403Error mimics an error that produces "403" text but is not an OpenAPI error or sentinel
type customFake403Error struct {
	msg string
}

func (e *customFake403Error) Error() string {
	return e.msg
}

// customErrorWithStatusCode has a StatusCode field but is a distinct type from oapierror.GenericOpenAPIError
type customErrorWithStatusCode struct {
	StatusCode int
}

func (e *customErrorWithStatusCode) Error() string {
	return fmt.Sprintf("status code: %d", e.StatusCode)
}

func TestIsDeleteForbidden_DeeplyWrappedAndAdversarialErrors(t *testing.T) {
	t.Run("nil error returns false", func(t *testing.T) {
		if IsDeleteForbidden(nil) {
			t.Fatal("expected IsDeleteForbidden(nil) == false")
		}
	})

	t.Run("deeply nested wrapped ErrDeleteInstanceForbidden (100 levels)", func(t *testing.T) {
		var deepErr error = ErrDeleteInstanceForbidden
		for i := 0; i < 100; i++ {
			deepErr = fmt.Errorf("wrap level %d: %w", i, deepErr)
		}
		if !IsDeleteForbidden(deepErr) {
			t.Fatalf("expected 100-level wrapped ErrDeleteInstanceForbidden to return true")
		}
	})

	t.Run("deeply nested wrapped OpenAPI 403 error (100 levels)", func(t *testing.T) {
		var deepErr error = oapierror.NewError(403, "Forbidden by IAM")
		for i := 0; i < 100; i++ {
			deepErr = fmt.Errorf("wrap level %d: %w", i, deepErr)
		}
		if !IsDeleteForbidden(deepErr) {
			t.Fatalf("expected 100-level wrapped OpenAPI 403 to return true")
		}
	})

	t.Run("errors.Join multi-error containing sentinel returns true", func(t *testing.T) {
		joined := errors.Join(
			errors.New("first error"),
			ErrDeleteInstanceForbidden,
			errors.New("third error"),
		)
		if !IsDeleteForbidden(joined) {
			t.Fatalf("expected errors.Join containing ErrDeleteInstanceForbidden to return true")
		}
	})

	t.Run("errors.Join multi-error containing OpenAPI 403 returns true", func(t *testing.T) {
		joined := errors.Join(
			errors.New("db disconnect"),
			oapierror.NewError(403, "Access denied"),
			errors.New("timeout"),
		)
		if !IsDeleteForbidden(joined) {
			t.Fatalf("expected errors.Join containing OpenAPI 403 to return true")
		}
	})

	t.Run("fake 403 error strings must return false (Adversarial String Attacks)", func(t *testing.T) {
		fakeErrors := []error{
			errors.New("403"),
			errors.New("403 Forbidden"),
			errors.New("HTTP 403 Forbidden"),
			errors.New("client received status 403"),
			errors.New("deleted 403 records"),
			errors.New("port 403403 is busy"),
			&customFake403Error{msg: "status: 403 forbidden: user cannot delete"},
			&customErrorWithStatusCode{StatusCode: 403},
			fmt.Errorf("nested fake: %w", &customFake403Error{msg: "403 Forbidden"}),
			errors.Join(errors.New("error 403"), errors.New("cannot delete instance")),
		}

		for i, err := range fakeErrors {
			if IsDeleteForbidden(err) {
				t.Fatalf("fake error #%d (%q) unexpectedly identified as forbidden!", i, err.Error())
			}
		}
	})

	t.Run("OpenAPI errors with non-403 status codes must return false", func(t *testing.T) {
		non403Codes := []int{100, 200, 301, 400, 401, 402, 404, 405, 408, 409, 429, 500, 502, 503, 504}
		for _, code := range non403Codes {
			oapiErr := oapierror.NewError(code, fmt.Sprintf("HTTP %d error", code))
			if IsDeleteForbidden(oapiErr) {
				t.Fatalf("OpenAPI status %d unexpectedly identified as forbidden!", code)
			}
			wrapped := fmt.Errorf("wrap: %w", oapiErr)
			if IsDeleteForbidden(wrapped) {
				t.Fatalf("Wrapped OpenAPI status %d unexpectedly identified as forbidden!", code)
			}
		}
	})

	t.Run("Concurrent IsDeleteForbidden evaluation stress", func(t *testing.T) {
		const numGoroutines = 100
		const iterations = 500
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		sentinel := fmt.Errorf("outer: %w", ErrDeleteInstanceForbidden)
		oapi403 := oapierror.NewError(403, "forbidden")
		fake := errors.New("fake 403")

		for g := 0; g < numGoroutines; g++ {
			go func(id int) {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					if !IsDeleteForbidden(sentinel) {
						t.Errorf("sentinel returned false")
					}
					if !IsDeleteForbidden(oapi403) {
						t.Errorf("oapi403 returned false")
					}
					if IsDeleteForbidden(fake) {
						t.Errorf("fake returned true")
					}
					if IsDeleteForbidden(nil) {
						t.Errorf("nil returned true")
					}
				}
			}(g)
		}

		wg.Wait()
	})
}
