# stackit-restore

[![Go Reference](https://pkg.go.dev/badge/github.com/GrigoreAlexandru/stackit-restore.svg)](https://pkg.go.dev/github.com/GrigoreAlexandru/stackit-restore)
[![Release](https://img.shields.io/github/v/release/GrigoreAlexandru/stackit-restore)](https://github.com/GrigoreAlexandru/stackit-restore/releases)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

An interactive Go CLI tool for PostgreSQL dump and restore workflows on [STACKIT](https://www.stackit.cloud/) PostgreSQL Flex. Supports arrow-key guided TUI navigation, Point-In-Time (PIT) snapshot creation, custom binary `.dump` format, cross-database restoration, and single-line non-interactive CLI commands.

---

## Features

- **Guided Interactive TUI**: Arrow-key terminal forms built with [charmbracelet/huh](https://github.com/charmbracelet/huh).
- **Single-Line CLI Flags**: Non-interactive command options for scripts, automation, and CI/CD pipelines.
- **Source & Destination Database Choices**: Restore from a production database directly into a staging or development database.
- **STACKIT Replica & PIT Cloning**: Clone PostgreSQL instances from backups or specific Point-In-Time (PIT) timestamps before dumping.
- **Custom Binary `.dump` & `pg_restore`**: Custom binary dump format (`pg_dump -Fc`) restored with `pg_restore` for max performance and safety (`--clean`, `--if-exists`, `--no-owner`, `--no-privileges`).
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
- **STACKIT Account & Service Account Token**: A STACKIT Service Account Bearer Token with PostgreSQL Flex permissions.

---

## Environment Configuration

Create a `.env` file or export environment variables:

```bash
# STACKIT Authentication & Region (Required)
STACKIT_PROJECT_ID=your-stackit-project-id
STACKIT_REGION=eu01
STACKIT_SERVICE_ACCOUNT_TOKEN=your-service-account-bearer-token

# Alternatively, mount token from a secret file:
# STACKIT_SERVICE_ACCOUNT_TOKEN_FILE=/path/to/token.txt

# PostgreSQL Credentials
STACKIT_POSTGRES_USER=postgres
STACKIT_POSTGRES_PASSWORD=your-postgres-password
STACKIT_POSTGRES_SSLMODE=require

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

1. Select **Source Database** (`Instance / Database`).
2. Select **Action**: `Dump` or `Restore`.
3. If `Restore`: Select **Destination Database** to restore into.
4. Select **Dump Mode** or **Restore Mode**:
   - **Dump Modes**:
     - *Dump from live data*
     - *Dump from stackit replica*
     - *Dump from stackit replica (PIT)*
   - **Restore Modes**:
     - *Restore from live db*
     - *Restore from Stackit backup*
     - *Restore from Stackit replica (PIT)*
     - *Restore from existing .dump file*
5. Review the **Summary & Command Explanation** and confirm execution.

---

### Single-Line Non-Interactive Mode

#### 1. Dump from Live Database
```bash
stackit-restore --action=dump --instance=Production --database=app_prod --mode=live
```

#### 2. Dump from STACKIT Replica at Point-In-Time (PIT)
```bash
stackit-restore --action=dump --instance=Production --database=app_prod --mode=pit --pit="2026-08-13 15:00:00"
```

#### 3. Restore Directly from Live Source DB into Target DB
```bash
stackit-restore --action=restore --instance=Production --database=app_prod --target-instance=Staging --target-database=app_stg --mode=live_db
```

#### 4. Restore from STACKIT Backup into Target DB
```bash
stackit-restore --action=restore --instance=Production --database=app_prod --target-instance=Staging --target-database=app_stg --mode=stackit_backup --backup=prod-auto-20260112
```

#### 5. Restore from STACKIT Replica at PIT Datetime into Target DB
```bash
stackit-restore --action=restore --instance=Production --database=app_prod --target-instance=Staging --target-database=app_stg --mode=pit --pit="2026-08-13 15:00:00"
```

#### 6. Restore from Local `.dump` File into Target DB
```bash
stackit-restore --action=restore --instance=Staging --database=app_stg --mode=dump_file --dump-file=/tmp/dumps/custom_dump.dump
```

---

## Flag Options Reference

| Flag | Description | Default |
| --- | --- | --- |
| `-h, --help` | Show help screen and usage examples | `false` |
| `--action` | Operation to perform: `dump` or `restore` | `""` |
| `--instance, --source-instance` | Source STACKIT PostgreSQL instance ID or Name | `""` |
| `--database, --source-database` | Source database name | `""` |
| `--target-instance, --dest-instance` | Destination STACKIT PostgreSQL instance ID or Name | Source Instance |
| `--target-database, --dest-database` | Destination database name | Source Database |
| `--mode` | Dump mode (`live`, `replica`, `pit`) or Restore mode (`live_db`, `stackit_backup`, `pit`, `dump_file`) | `""` |
| `--pit` | Point-In-Time datetime string (`YYYY-MM-DD HH:MM:SS` or `RFC3339`) | `""` |
| `--backup` | STACKIT backup name | `""` |
| `--dump-file` | Path to local `.dump` file | `""` |

---

## License

Distributed under the Apache License 2.0. See [LICENSE](LICENSE) for details.
