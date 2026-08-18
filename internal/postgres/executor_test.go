package postgres

import (
	"testing"
)

func TestSanitizeInstanceName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Production", "PRODUCTION"},
		{"prod-db-01", "PROD_DB_01"},
		{"staging.database", "STAGING_DATABASE"},
		{"  my app db  ", "MY_APP_DB"},
	}

	for _, tt := range tests {
		got := SanitizeInstanceName(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeInstanceName(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestBuildInstanceHost(t *testing.T) {
	host := BuildInstanceHost("inst-12345", "eu01")
	expected := "inst-12345.postgresql.eu01.onstackit.cloud"
	if host != expected {
		t.Fatalf("expected host %q, got %q", expected, host)
	}
}

func TestResolveCredentialsForLocal(t *testing.T) {
	t.Setenv("LOCAL_USER", "loc_admin")
	t.Setenv("LOCAL_PASS", "loc_secret")
	t.Setenv("LOCAL_SSLMODE", "disable")

	creds, err := ResolveCredentials("local")
	if err != nil {
		t.Fatalf("unexpected error resolving local credentials: %v", err)
	}

	if creds.User != "loc_admin" || creds.Password != "loc_secret" || creds.SSLMode != "disable" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
}

func TestResolveCredentialsForInstance(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "prod_admin")
	t.Setenv("PRODUCTION_PASS", "prod_pass_123")

	creds, err := ResolveCredentials("Production")
	if err != nil {
		t.Fatalf("unexpected error resolving instance credentials: %v", err)
	}

	if creds.User != "prod_admin" || creds.Password != "prod_pass_123" || creds.SSLMode != "require" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
}

func TestResolveCredentialsFailsWithoutFallbacks(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "")
	t.Setenv("PRODUCTION_PASS", "")
	t.Setenv("STACKIT_POSTGRES_USER", "fallback_user")
	t.Setenv("STACKIT_POSTGRES_PASSWORD", "fallback_pass")

	_, err := ResolveCredentials("Production")
	if err == nil {
		t.Fatal("expected error when PRODUCTION_USER/PASS is missing, but got nil (fallback was incorrectly used)")
	}
}

func TestHasCredentialsAndHint(t *testing.T) {
	t.Setenv("STAGING_USER", "")
	t.Setenv("STAGING_PASS", "")
	t.Setenv("LOCAL_USER", "")
	t.Setenv("LOCAL_PASS", "")

	if HasCredentials("Staging") {
		t.Fatal("expected HasCredentials(Staging) to be false when env unset")
	}
	hint := GetMissingCredentialsHint("Staging")
	if hint != "STAGING_USER and STAGING_PASS" {
		t.Fatalf("unexpected hint: %q", hint)
	}

	localHint := GetMissingCredentialsHint("local")
	if localHint != "LOCAL_USER and LOCAL_PASS" {
		t.Fatalf("unexpected local hint: %q", localHint)
	}

	t.Setenv("STAGING_USER", "stg_user")
	t.Setenv("STAGING_PASS", "stg_pass")
	if !HasCredentials("Staging") {
		t.Fatal("expected HasCredentials(Staging) to be true when env is set")
	}
}

func TestGetLocalEndpoint(t *testing.T) {
	t.Setenv("LOCAL_HOST", "127.0.0.1")
	t.Setenv("LOCAL_PORT", "5433")

	host, port, err := GetLocalEndpoint()
	if err != nil {
		t.Fatalf("unexpected error getting local endpoint: %v", err)
	}

	if host != "127.0.0.1" || port != 5433 {
		t.Fatalf("expected 127.0.0.1:5433, got %s:%d", host, port)
	}
}

func TestGetLocalEndpointFailsWhenMissing(t *testing.T) {
	t.Setenv("LOCAL_HOST", "")
	t.Setenv("LOCAL_PORT", "")

	_, _, err := GetLocalEndpoint()
	if err == nil {
		t.Fatal("expected error when LOCAL_HOST/LOCAL_PORT are unset, got nil")
	}
}

func TestIsIgnorableRestoreWarning(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected bool
	}{
		{
			name:     "missing pg_stat_kcache extension error",
			output:   "pg_restore: creating EXTENSION \"pg_stat_kcache\"\npg_restore: error: could not execute query: ERROR: extension \"pg_stat_kcache\" is not available\npg_restore: warning: errors were ignored during processing",
			expected: true,
		},
		{
			name:     "missing extension control file",
			output:   "ERROR: could not open extension control file \"/usr/share/postgresql/extension/pg_stat_kcache.control\": No such file or directory\nwarning: errors were ignored during processing",
			expected: true,
		},
		{
			name:     "permission denied to create extension",
			output:   "ERROR: permission denied to create extension \"pg_stat_statements\"\nHINT: Must be superuser to create this extension.",
			expected: true,
		},
		{
			name:     "fatal connection failure",
			output:   "pg_restore: error: connection to server at \"127.0.0.1\", port 5432 failed: Connection refused\npg_restore: error: could not connect to server",
			expected: false,
		},
		{
			name:     "fatal password authentication failure",
			output:   "FATAL: password authentication failed for user \"postgres\"",
			expected: false,
		},
		{
			name:     "fatal database does not exist",
			output:   "FATAL: database \"nonexistent\" does not exist",
			expected: false,
		},
		{
			name:     "empty output",
			output:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsIgnorableRestoreWarning(tt.output)
			if got != tt.expected {
				t.Errorf("IsIgnorableRestoreWarning() = %v, want %v", got, tt.expected)
			}
		})
	}
}

