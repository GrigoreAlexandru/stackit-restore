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
		"--instance=Staging",
		"--database=app_stg",
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
	if opts.Mode != "restore_from_stackit_backup" {
		t.Errorf("expected normalized mode 'restore_from_stackit_backup', got %q", opts.Mode)
	}
	if opts.Backup != "stg-auto-20260112" {
		t.Errorf("expected backup 'stg-auto-20260112', got %q", opts.Backup)
	}
	if opts.TargetInstance != "Staging" {
		t.Errorf("expected TargetInstance to default to 'Staging', got %q", opts.TargetInstance)
	}
	if opts.TargetDatabase != "app_stg" {
		t.Errorf("expected TargetDatabase to default to 'app_stg', got %q", opts.TargetDatabase)
	}
}

func TestParseOptions_TargetInstanceCrossCloudAllowedWithCredentials(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "prod_user")
	t.Setenv("PRODUCTION_PASS", "prod_pass")
	t.Setenv("STAGING_USER", "stg_user")
	t.Setenv("STAGING_PASS", "stg_pass")

	args := []string{
		"--action=restore",
		"--instance=Production",
		"--database=app_prod",
		"--target-instance=Staging",
		"--target-database=app_stg",
		"--mode=live_db",
	}

	opts, err := ParseOptions(args)
	if err != nil {
		t.Fatalf("unexpected error parsing target options with Staging target: %v", err)
	}

	if opts.Instance != "Production" || opts.Database != "app_prod" {
		t.Errorf("expected source Production/app_prod, got %s/%s", opts.Instance, opts.Database)
	}
	if opts.TargetInstance != "Staging" || opts.TargetDatabase != "app_stg" {
		t.Errorf("expected target Staging/app_stg, got %s/%s", opts.TargetInstance, opts.TargetDatabase)
	}
}

func TestParseOptions_LocalSourceToLocalTarget(t *testing.T) {
	t.Setenv("LOCAL_USER", "loc_user")
	t.Setenv("LOCAL_PASS", "loc_pass")

	args := []string{
		"--action=restore",
		"--instance=local",
		"--database=app_source",
		"--target-instance=local",
		"--target-database=app_dest",
		"--mode=live_db",
	}

	opts, err := ParseOptions(args)
	if err != nil {
		t.Fatalf("unexpected error parsing local to local restore: %v", err)
	}

	if opts.Instance != "local" || opts.TargetInstance != "local" {
		t.Errorf("expected local to local, got %s -> %s", opts.Instance, opts.TargetInstance)
	}
}

func TestParseOptions_ValidationErrors(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "prod_user")
	t.Setenv("PRODUCTION_PASS", "prod_pass")
	t.Setenv("STAGING_USER", "stg_user")
	t.Setenv("STAGING_PASS", "stg_pass")
	t.Setenv("UNCONFIGURED_USER", "")
	t.Setenv("UNCONFIGURED_PASS", "")

	invalidCases := []struct {
		name string
		args []string
	}{
		{
			name: "missing instance",
			args: []string{"--action=dump", "--database=app_prod"},
		},
		{
			name: "missing database",
			args: []string{"--action=dump", "--instance=Production"},
		},
		{
			name: "missing pit datetime for pit dump mode",
			args: []string{"--action=dump", "--instance=Production", "--database=app_prod", "--mode=pit"},
		},
		{
			name: "missing credentials for source instance",
			args: []string{"--action=dump", "--instance=Unconfigured", "--database=app_prod", "--mode=live"},
		},
		{
			name: "missing credentials for destination instance",
			args: []string{"--action=restore", "--instance=Production", "--database=app_prod", "--target-instance=Unconfigured", "--target-database=app_dest", "--mode=live_db"},
		},
		{
			name: "missing backup name for backup restore mode",
			args: []string{"--action=restore", "--instance=Staging", "--database=app_stg", "--mode=backup"},
		},
		{
			name: "missing dump file path for dump_file restore mode",
			args: []string{"--action=restore", "--instance=Staging", "--database=app_stg", "--mode=dump_file"},
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
