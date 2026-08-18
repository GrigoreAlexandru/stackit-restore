package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestParseOptions_Help(t *testing.T) {
	opts, err := ParseOptions([]string{"--help"})
	if err != nil {
		t.Fatalf("unexpected error parsing --help: %v", err)
	}
	if !opts.Help {
		t.Errorf("expected Help to be true")
	}

	buf := new(bytes.Buffer)
	PrintUsage(buf)
	if !strings.Contains(buf.String(), "PostgreSQL Restore CLI for STACKIT") {
		t.Errorf("expected help output to contain application description")
	}
}

func TestParseOptions_SingleLineDump(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "prod_user")
	t.Setenv("PRODUCTION_PASS", "prod_pass")

	args := []string{
		"--action=dump",
		"--instance=Production",
		"--database=app_prod",
		"--mode=pit",
		"--pit=2026-08-13 15:00:00",
	}

	opts, err := ParseOptions(args)
	if err != nil {
		t.Fatalf("unexpected error parsing dump options: %v", err)
	}

	if !opts.NonInteractive {
		t.Errorf("expected NonInteractive to be true")
	}
	if opts.Action != "dump" {
		t.Errorf("expected action 'dump', got %q", opts.Action)
	}
	if opts.Instance != "Production" {
		t.Errorf("expected instance 'Production', got %q", opts.Instance)
	}
	if opts.Database != "app_prod" {
		t.Errorf("expected database 'app_prod', got %q", opts.Database)
	}
	if opts.Mode != "dump_from_pit" {
		t.Errorf("expected normalized mode 'dump_from_pit', got %q", opts.Mode)
	}
	if opts.PITParsed == nil {
		t.Fatalf("expected PITParsed to be non-nil")
	}
	expectedPIT := time.Date(2026, 8, 13, 15, 0, 0, 0, time.UTC)
	if !opts.PITParsed.Equal(expectedPIT) {
		t.Errorf("expected PIT timestamp %v, got %v", expectedPIT, *opts.PITParsed)
	}
}

func TestParseOptions_SingleLineRestoreFromBackup(t *testing.T) {
	t.Setenv("STAGING_USER", "stg_user")
	t.Setenv("STAGING_PASS", "stg_pass")

	args := []string{
		"--action=restore",
		"--target-instance=Staging",
		"--target-database=app_stg",
		"--mode=backup",
		"--backup=stg-auto-20260112",
	}

	opts, err := ParseOptions(args)
	if err != nil {
		t.Fatalf("unexpected error parsing restore options: %v", err)
	}

	if !opts.NonInteractive {
		t.Errorf("expected NonInteractive to be true")
	}
	if opts.Action != "restore" {
		t.Errorf("expected action 'restore', got %q", opts.Action)
	}
	if opts.Mode != RestoreModeBackup {
		t.Errorf("expected normalized mode %q, got %q", RestoreModeBackup, opts.Mode)
	}
	if opts.Backup != "stg-auto-20260112" {
		t.Errorf("expected backup 'stg-auto-20260112', got %q", opts.Backup)
	}
	if opts.TargetInstance != "Staging" {
		t.Errorf("expected TargetInstance 'Staging', got %q", opts.TargetInstance)
	}
	if opts.TargetDatabase != "app_stg" {
		t.Errorf("expected TargetDatabase 'app_stg', got %q", opts.TargetDatabase)
	}
}

func TestParseOptions_SingleLineRestoreFromDumpFile(t *testing.T) {
	t.Setenv("LOCAL_USER", "loc_user")
	t.Setenv("LOCAL_PASS", "loc_pass")

	args := []string{
		"--action=restore",
		"--target-instance=local",
		"--target-database=app_local",
		"--dump-file=dumps/custom.dump",
	}

	opts, err := ParseOptions(args)
	if err != nil {
		t.Fatalf("unexpected error parsing restore dump file: %v", err)
	}

	if opts.Action != "restore" {
		t.Errorf("expected action 'restore', got %q", opts.Action)
	}
	if opts.Mode != RestoreModeDumpFile {
		t.Errorf("expected normalized mode %q, got %q", RestoreModeDumpFile, opts.Mode)
	}
	if opts.DumpFile != "dumps/custom.dump" {
		t.Errorf("expected dump file 'dumps/custom.dump', got %q", opts.DumpFile)
	}
}

func TestParseOptions_SingleLineSync(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "prod_user")
	t.Setenv("PRODUCTION_PASS", "prod_pass")
	t.Setenv("STAGING_USER", "stg_user")
	t.Setenv("STAGING_PASS", "stg_pass")

	args := []string{
		"--action=sync",
		"--instance=Production",
		"--database=app_prod",
		"--target-instance=Staging",
		"--target-database=app_stg",
		"--mode=live",
	}

	opts, err := ParseOptions(args)
	if err != nil {
		t.Fatalf("unexpected error parsing sync options: %v", err)
	}

	if opts.Action != "sync" {
		t.Errorf("expected action 'sync', got %q", opts.Action)
	}
	if opts.Instance != "Production" || opts.Database != "app_prod" {
		t.Errorf("expected source Production/app_prod, got %s/%s", opts.Instance, opts.Database)
	}
	if opts.TargetInstance != "Staging" || opts.TargetDatabase != "app_stg" {
		t.Errorf("expected target Staging/app_stg, got %s/%s", opts.TargetInstance, opts.TargetDatabase)
	}
}

func TestParseOptions_ActionAliases(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "prod_user")
	t.Setenv("PRODUCTION_PASS", "prod_pass")
	t.Setenv("LOCAL_USER", "loc_user")
	t.Setenv("LOCAL_PASS", "loc_pass")

	// Export -> dump
	opts, err := ParseOptions([]string{"--action=export", "--instance=Production", "--database=app_prod"})
	if err != nil || opts.Action != "dump" {
		t.Fatalf("expected action 'dump' from export, got %q, err: %v", opts.Action, err)
	}

	// Import -> restore
	opts, err = ParseOptions([]string{"--action=import", "--target-instance=local", "--target-database=app_local", "--dump-file=dumps/test.dump"})
	if err != nil || opts.Action != "restore" {
		t.Fatalf("expected action 'restore' from import, got %q, err: %v", opts.Action, err)
	}

	// Copy -> sync
	opts, err = ParseOptions([]string{"--action=copy", "--instance=Production", "--database=app_prod", "--target-instance=local", "--target-database=app_local"})
	if err != nil || opts.Action != "sync" {
		t.Fatalf("expected action 'sync' from copy, got %q, err: %v", opts.Action, err)
	}
}

func TestParseOptions_LocalCapabilitiesValidation(t *testing.T) {
	t.Setenv("LOCAL_USER", "loc_user")
	t.Setenv("LOCAL_PASS", "loc_pass")

	// Dump replica on local rejected
	_, err := ParseOptions([]string{"--action=dump", "--instance=local", "--database=app_local", "--mode=replica"})
	if err == nil {
		t.Fatal("expected validation error for dump replica on local")
	}

	// Dump PIT on local rejected
	_, err = ParseOptions([]string{"--action=dump", "--instance=local", "--database=app_local", "--mode=pit", "--pit=2026-08-13 15:00:00"})
	if err == nil {
		t.Fatal("expected validation error for dump PIT on local")
	}

	// Sync replica from local rejected
	t.Setenv("PRODUCTION_USER", "prod_user")
	t.Setenv("PRODUCTION_PASS", "prod_pass")
	_, err = ParseOptions([]string{"--action=sync", "--instance=local", "--database=app_local", "--target-instance=Production", "--target-database=app_prod", "--mode=replica"})
	if err == nil {
		t.Fatal("expected validation error for sync replica from local")
	}

	// Restore from backup on local source rejected
	_, err = ParseOptions([]string{"--action=restore", "--instance=local", "--target-instance=local", "--target-database=app_local", "--mode=backup", "--backup=backup-1"})
	if err == nil {
		t.Fatal("expected validation error for restore from backup on local")
	}
}

func TestParseOptions_ValidationErrors(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "prod_user")
	t.Setenv("PRODUCTION_PASS", "prod_pass")
	t.Setenv("STAGING_USER", "stg_user")
	t.Setenv("STAGING_PASS", "stg_pass")

	invalidCases := []struct {
		name string
		args []string
	}{
		{
			name: "missing database for dump",
			args: []string{"--action=dump", "--instance=Production"},
		},
		{
			name: "missing pit datetime for pit dump mode",
			args: []string{"--action=dump", "--instance=Production", "--database=app_prod", "--mode=pit"},
		},
		{
			name: "missing credentials for dump",
			args: []string{"--action=dump", "--instance=Unconfigured", "--database=app_prod"},
		},
		{
			name: "missing dump-file for restore",
			args: []string{"--action=restore", "--target-instance=Staging", "--target-database=app_stg", "--mode=dump_file"},
		},
		{
			name: "missing backup name for restore backup",
			args: []string{"--action=restore", "--instance=Production", "--target-instance=Staging", "--target-database=app_stg", "--mode=backup"},
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseOptions(tc.args)
			if err == nil {
				t.Errorf("expected validation error for case %q, got nil", tc.name)
			}
		})
	}
}
