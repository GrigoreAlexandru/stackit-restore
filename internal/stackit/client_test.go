package stackit

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
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

	oapi403 := oapierror.NewError(403, "Forbidden")
	if !IsDeleteForbidden(oapi403) {
		t.Error("expected GenericOpenAPIError with 403 to be recognized as forbidden")
	}

	oapi500 := oapierror.NewError(500, "Internal Server Error")
	if IsDeleteForbidden(oapi500) {
		t.Error("expected GenericOpenAPIError with 500 not to be recognized as forbidden")
	}

	if IsDeleteForbidden(errors.New("500 Internal Server Error")) {
		t.Error("expected 500 error not to be recognized as forbidden")
	}

	if IsDeleteForbidden(nil) {
		t.Error("expected nil error not to be recognized as forbidden")
	}
}

func TestIsNotFound(t *testing.T) {
	oapi404 := oapierror.NewError(404, "Not Found")
	if !IsNotFound(oapi404) {
		t.Error("expected GenericOpenAPIError with 404 to be recognized as not found")
	}

	if !IsNotFound(errors.New("404 not found: resource missing")) {
		t.Error("expected string containing 404 not found to be recognized as not found")
	}

	if IsNotFound(oapierror.NewError(500, "Internal Server Error")) {
		t.Error("expected 500 error not to be recognized as not found")
	}

	if IsNotFound(nil) {
		t.Error("expected nil error not to be recognized as not found")
	}
}

func TestClient_SetOutputWriterAndLogf(t *testing.T) {
	var buf strings.Builder
	c := &Client{}
	c.SetOutputWriter(&buf)

	c.logf("Test message %d\n", 42)
	if !strings.Contains(buf.String(), "Test message 42") {
		t.Fatalf("expected logged message, got %q", buf.String())
	}
}

func TestClient_Region(t *testing.T) {
	c := &Client{region: "eu01"}
	if c.Region() != "eu01" {
		t.Fatalf("expected region eu01, got %q", c.Region())
	}
}

func TestWaitForEndpointReady_LocalListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create local test listener: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	c := &Client{}
	var logBuf strings.Builder
	c.SetOutputWriter(&logBuf)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.WaitForEndpointReady(ctx, "127.0.0.1", int32(addr.Port)); err != nil {
		t.Fatalf("expected WaitForEndpointReady to succeed on local listener, got: %v", err)
	}
}

func TestWaitForEndpointReady_Timeout(t *testing.T) {
	c := &Client{}
	var logBuf strings.Builder
	c.SetOutputWriter(&logBuf)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Use an unresolvable or non-listening port
	err := c.WaitForEndpointReady(ctx, "127.0.0.1", 65530)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}


