package stackit

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatTemporaryCloneName(t *testing.T) {
	tests := []struct {
		name       string
		sourceName string
		wantPrefix string
	}{
		{
			name:       "Standard lowercase",
			sourceName: "production",
			wantPrefix: "production-temp-clone-",
		},
		{
			name:       "Mixed case with spaces and symbols",
			sourceName: "App DB Staging #1",
			wantPrefix: "app-db-staging--1-temp-clone-",
		},
		{
			name:       "Empty string",
			sourceName: "",
			wantPrefix: "source-temp-clone-",
		},
		{
			name:       "Very long name truncated to fit 63 limit",
			sourceName: "this-is-a-very-long-instance-name-that-exceeds-the-maximum-length",
			wantPrefix: "this-is-a-very-long-instance-name-that-temp-clone-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTemporaryCloneName(tt.sourceName)
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("expected prefix %q, got %q", tt.wantPrefix, got)
			}
			if len(got) > 63 {
				t.Errorf("expected length <= 63, got %d for %q", len(got), got)
			}
		})
	}
}

func TestIsDeleteForbidden(t *testing.T) {
	if !IsDeleteForbidden(ErrDeleteInstanceForbidden) {
		t.Error("expected ErrDeleteInstanceForbidden to be recognized as forbidden")
	}

	if !IsDeleteForbidden(errors.New("request failed: 403 Forbidden - user lacks role")) {
		t.Error("expected string containing 403 Forbidden to be recognized as forbidden")
	}

	if IsDeleteForbidden(errors.New("500 Internal Server Error")) {
		t.Error("expected 500 error not to be recognized as forbidden")
	}

	if IsDeleteForbidden(nil) {
		t.Error("expected nil error not to be recognized as forbidden")
	}
}

