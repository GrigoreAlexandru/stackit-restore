# stackit-restore

[![Go Reference](https://pkg.go.dev/badge/github.com/GrigoreAlexandru/stackit-restore.svg)](https://pkg.go.dev/github.com/GrigoreAlexandru/stackit-restore)
[![Release](https://img.shields.io/github/v/release/GrigoreAlexandru/stackit-restore)](https://github.com/GrigoreAlexandru/stackit-restore/releases)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

An interactive Go CLI tool for PostgreSQL workflows across [STACKIT](https://www.stackit.cloud/) PostgreSQL Flex cloud instances and local database instances. Supports arrow-key guided TUI navigation, intent-first user workflows (**Dump**, **Restore**, **Sync**), Point-In-Time (PIT) snapshot creation, custom binary `.dump` format, dynamic per-instance credentials, and single-line non-interactive CLI commands.

---

## Core Workflows

- **Dump to File (Export)**: Save any live database, STACKIT backup replica, or Point-In-Time snapshot to a portable `.dump` file.
- **Restore to Database (Import)**: Restore a local `.dump` file, automated STACKIT cloud backup, or Point-In-Time snapshot directly into a target database.
- **Sync Databases (Copy)**: Direct database-to-database replication (e.g. refresh local or staging from production) in a single command.

---

## Features

- **Guided Interactive TUI**: Arrow-key terminal forms built with [charmbracelet/huh](https://github.com/charmbracelet/huh).
- **Intent-Driven UX**: Clear, goal-oriented actions (**Dump**, **Restore**, **Sync**) with dynamic step-by-step guidance.
- **Dynamic DSN & Host Construction**:
  - STACKIT host: `[instance-id].postgresql.[region].onstackit.cloud` (port `5432`).
  - Local host: `LOCAL_HOST` and `LOCAL_PORT`.
- **Dynamic Per-Instance Credentials**: Credentials resolved dynamically from environment variables formatted as `[INSTANCE_NAME]_USER` and `[INSTANCE_NAME]_PASS` (e.g. `PRODUCTION_USER` & `PRODUCTION_PASS`).
- **Credential-Based Availability**: Any instance can be used as source or destination as long as credentials are configured for it. Unconfigured instances are visible in the TUI menu but marked as `(unavailable: missing <NAME>_USER and <NAME>_PASS)`.
- **Local Database Support**: `local` is available as a first-class option using `LOCAL_USER` and `LOCAL_PASS` (guarded for live operations only).
- **Custom Binary `.dump` & Safe `pg_restore`**: Custom binary dump format (`pg_dump -Fc`) restored with `pg_restore` using safe production defaults:
  - `--clean --if-exists`: Drops existing database objects before recreating them, avoiding duplicate table/key errors on restore.
  - `--no-owner`: Do not output commands to set ownership of objects to match the original database. This prevents permission errors when restoring across different database users/roles (e.g. from cloud instances into local or staging).
  - `--no-privileges` (alias `--no-acl`): Prevents restoring access privileges (GRANT/REVOKE commands) from the source database, preserving the destination database's security and role configurations without permission conflicts.
- **Step-by-Step Progress Tracking & Checkmarks**: All planned execution steps are listed upfront and dynamically updated with styled status indicators (`[✓]` Completed, `[>]` In Progress, `[ ]` Pending, `[✗]` Failed).
- **Live Command Output Streaming**: `pg_dump`, `pg_restore`, and STACKIT provisioning outputs stream live directly underneath each active step.
- **Automated Error Logging (`dumps/logs/`)**: When errors occur during execution, a detailed error log with full captured command outputs and execution context is automatically generated in `dumps/logs/error_<TIMESTAMP>_<ACTION>.log`.
- **Confirmation & Explanation**: Plain-English execution plan presented prior to executing actions.

---

## Installation

### Method 1: `go install` (Recommended for Go Developers)
```bash
go install github.com/GrigoreAlexandru/stackit-restore/cmd/stackit-restore@latest
```

### Method 2: Shell Installer (Linux & macOS)
```bash
curl -sSL https://raw.githubusercontent.com/GrigoreAlexandru/stackit-restore/main/install.sh | bash
```

### Method 3: Pre-built Binary Downloads
Download pre-compiled binaries for Linux (amd64/arm64), macOS (Intel/Apple Silicon), and Windows from [GitHub Releases](https://github.com/GrigoreAlexandru/stackit-restore/releases/latest).

### Method 4: Build from Source
```bash
git clone https://github.com/GrigoreAlexandru/stackit-restore.git
cd stackit-restore
go build -o stackit-restore ./cmd/stackit-restore
```

---

## Prerequisites

- **PostgreSQL Client Tools**: `pg_dump` and `pg_restore` (part of `postgresql-client` package) must be installed and available in your `PATH`.
- **STACKIT Service Account Key JSON**: Service account key with PostgreSQL Flex permissions (if managing STACKIT instances).

---

## Environment Configuration

Create a `.env` file or export environment variables:

```bash
# STACKIT Authentication & Region (Required for STACKIT instances)
STACKIT_PROJECT_ID=your-stackit-project-id
STACKIT_REGION=eu01
STACKIT_SERVICE_ACCOUNT_KEY_PATH=/path/to/service_account_key.json

# Dynamic Per-Instance PostgreSQL Credentials ([INSTANCE_NAME]_USER and [INSTANCE_NAME]_PASS)
PRODUCTION_USER=prod_admin
PRODUCTION_PASS=prod_secret_pass
STAGING_USER=stg_admin
STAGING_PASS=stg_secret_pass

# Local Database Configuration
LOCAL_HOST=localhost
LOCAL_PORT=5432
LOCAL_DB=postgres
LOCAL_USER=postgres
LOCAL_PASS=postgres_secret

# Local Dump Directory (Default: dumps)
POSTGRES_DUMP_DIR=dumps
```

---

## Usage

### Guided Interactive Mode (TUI)
Simply run the binary without arguments to launch the guided arrow-key menu:
```bash
stackit-restore
```

1. **What would you like to do?**
   - `Dump to File`
   - `Restore to Database`
   - `Sync Databases`
2. Follow the dynamic, context-aware steps for your chosen action.
3. Review the **Summary & Command Explanation** and confirm execution.

---

### Single-Line Non-Interactive Mode

#### 1. Dump to File (Export)
```bash
# Live export from cloud instance:
stackit-restore --action=dump --instance=Production --database=app_prod --mode=live

# Live export from local database:
stackit-restore --action=dump --instance=local --database=app_local --mode=live

# Export from cloud replica at Point-In-Time:
stackit-restore --action=dump --instance=Production --database=app_prod --mode=pit --pit="2026-08-13 15:00:00"
```

#### 2. Restore to Database (Import)
```bash
# Restore local .dump file into local database:
stackit-restore --action=restore --target-instance=local --target-database=app_local --mode=dump_file --dump-file=dumps/my_dump.dump

# Restore a STACKIT automated cloud backup into local database:
stackit-restore --action=restore --instance=Production --backup=prod-auto-20260112 --target-instance=local --target-database=app_local --mode=backup

# Restore a STACKIT Point-In-Time snapshot into Staging database:
stackit-restore --action=restore --instance=Production --target-instance=Staging --target-database=app_stg --mode=pit --pit="2026-08-13 15:00:00"
```

#### 3. Sync Databases (Direct DB $\rightarrow$ DB)
```bash
# Direct live sync from Production to local:
stackit-restore --action=sync --instance=Production --database=app_prod --target-instance=local --target-database=app_local --mode=live

# Direct sync from Production backup replica into Staging:
stackit-restore --action=sync --instance=Production --database=app_prod --target-instance=Staging --target-database=app_stg --mode=replica
```

---

## Flag Options Reference

| Flag | Description | Default |
| --- | --- | --- |
| `-h, --help` | Show help screen and usage examples | `false` |
| `--action` | Operation to perform: `dump` (export), `restore` (import), or `sync` (copy) | `""` |
| `--instance, --source-instance` | Source PostgreSQL instance ID/Name or `'local'` | `""` |
| `--database, --source-database` | Source database name | `""` |
| `--target-instance, --dest-instance` | Destination PostgreSQL instance ID/Name or `'local'` | `""` |
| `--target-database, --dest-database` | Destination database name | `""` |
| `--mode` | Extraction / Restore mode | `""` |
| `--pit` | Point-In-Time datetime string (`YYYY-MM-DD HH:MM:SS` or `RFC3339`) | `""` |
| `--backup` | STACKIT backup name | `""` |
| `--dump-file` | Path to local `.dump` file | `""` |
| `--sa-key-path` | Path to STACKIT Service Account Key JSON file | `""` |

---

## License

Distributed under the Apache License 2.0. See [LICENSE](LICENSE) for details.
