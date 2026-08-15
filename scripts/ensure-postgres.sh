#!/usr/bin/env bash
# Ensure a local PostgreSQL (native, no Docker) is running and the target
# database exists.
#
# RimeDeck fork policy: zero Docker. The dev PostgreSQL comes from the same
# pre-built binaries the desktop app bundles (apps/desktop/resources/pgsql,
# fetched by apps/desktop/scripts/bundle-pg.mjs), falling back to a system
# PostgreSQL on PATH. The data directory lives under .rimedeck-dev/pg-data in
# the repo (gitignored), so main checkouts and worktrees share one instance
# and isolate per-database, exactly like the old shared docker container did.
set -euo pipefail

ENV_FILE="${1:-.env}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
REPO_ROOT="$(pwd)"
# Git-for-Windows may report the repo root in Windows form (C:\...) while the
# shell addresses the same tree as /c/... (and WSL-style /mnt/c/... also
# exists on this setup). Normalize via cygpath when available.
if command -v cygpath >/dev/null 2>&1 && [ "$REPO_ROOT" != "${REPO_ROOT#C:}" ] 2>/dev/null; then
  REPO_ROOT="$(cygpath -u "$REPO_ROOT")"
elif [ -d /c ] && [ "$REPO_ROOT" != "${REPO_ROOT#C:}" ] 2>/dev/null; then
  REPO_ROOT="/c${REPO_ROOT#C:}"
fi
REPO_ROOT="${REPO_ROOT//\\//}"

# Windows binaries carry a .exe suffix; probe for it (uname alone is
# unreliable across Git-for-Windows / WSL interop on this repo's setups).
exe_suffix=""
if [ -e "$REPO_ROOT/apps/desktop/resources/pgsql/bin/pg_isready.exe" ]; then
  exe_suffix=".exe"
elif command -v pg_isready.exe >/dev/null 2>&1; then
  exe_suffix=".exe"
fi

pg_bin_dir=""
for candidate in \
  "$REPO_ROOT/apps/desktop/resources/pgsql/bin" \
  "$(command -v pg_ctl"$exe_suffix" >/dev/null 2>&1 && dirname "$(command -v pg_ctl"$exe_suffix")")"; do
  if [ -n "$candidate" ] && [ -e "$candidate/pg_isready$exe_suffix" ]; then
    pg_bin_dir="$candidate"
    break
  fi
done

if [ -z "$pg_bin_dir" ]; then
  echo "✗ No local PostgreSQL found."
  echo "  Run: node apps/desktop/scripts/bundle-pg.mjs   (downloads the bundled PG 17)"
  echo "  or install PostgreSQL on PATH."
  exit 1
fi

PGBIN() { echo "$pg_bin_dir/$1$exe_suffix"; }

# ---------- Parse DATABASE_URL for host/port/db (remote detection) ----------
db_host=""
db_port="$POSTGRES_PORT"
db_name="$POSTGRES_DB"

if [ -n "$DATABASE_URL" ]; then
  rest="${DATABASE_URL#*://}"
  rest="${rest%%\?*}"
  authority="${rest%%/*}"
  path="${rest#*/}"
  hostport="${authority##*@}"
  db_host="${hostport%%:*}"
  if [[ "$hostport" == *:* ]] && [ -n "${hostport##*:}" ]; then
    db_port="${hostport##*:}"
  fi
  if [ -n "$path" ]; then
    db_name="${path%%/*}"
  fi
fi

is_remote() {
  [ -n "$db_host" ] && [ "$db_host" != "localhost" ] && [ "$db_host" != "127.0.0.1" ] && [ "$db_host" != "::1" ]
}

# ---------- Remote: verify only ----------
if is_remote; then
  echo "==> Remote database detected (host: $db_host). Skipping local PostgreSQL."
  if [ -x "$(PGBIN pg_isready)" ] || [ -x "$(PGBIN pg_isready.exe)" ]; then
    echo "==> Waiting for PostgreSQL at $db_host:$db_port..."
    until "$(PGBIN pg_isready)" -h "$db_host" -p "$db_port" >/dev/null 2>&1; do sleep 1; done
    echo "✓ PostgreSQL ready (remote). Database: $db_name"
  else
    echo "✓ PostgreSQL configured (remote: $db_host:$db_port). Database: $db_name"
  fi
  exit 0
fi

# ---------- Local: manage the shared native instance ----------
DATA_DIR="$REPO_ROOT/.rimedeck-dev/pg-data"
LOG_DIR="$REPO_ROOT/.rimedeck-dev/pg-log"
mkdir -p "$LOG_DIR"

pg_is_up() {
  "$(PGBIN pg_isready)" -h localhost -p "$db_port" >/dev/null 2>&1
}

if ! pg_is_up; then
  if [ ! -f "$DATA_DIR/PG_VERSION" ]; then
    echo "==> Initializing local PostgreSQL data directory at $DATA_DIR..."
    mkdir -p "$DATA_DIR"
    share_dir="$(dirname "$pg_bin_dir")/share"
    "$(PGBIN initdb)" \
      --auth=trust --encoding=UTF8 --locale=C \
      ${share_dir:+-L "$share_dir"} \
      -D "$DATA_DIR" >/dev/null
  fi
  echo "==> Starting local PostgreSQL on port $db_port..."
  "$(PGBIN pg_ctl)" start -D "$DATA_DIR" -w -t 30 \
    -l "$LOG_DIR/postgresql.log" \
    -o "-p $db_port -c listen_addresses=localhost" >/dev/null
fi

echo "==> Waiting for PostgreSQL to be ready..."
until pg_is_up; do sleep 1; done

echo "==> Ensuring database '$db_name' exists..."
# trust auth: connect as the bootstrapped superuser; initdb --auth=trust makes
# the OS user the superuser, so connect without -U and let the default apply.
db_exists="$("$(PGBIN psql)" -h localhost -p "$db_port" -d postgres -Atqc \
  "SELECT 1 FROM pg_database WHERE datname = '$db_name'" 2>/dev/null || true)"

if [ "$db_exists" != "1" ]; then
  "$(PGBIN createdb)" -h localhost -p "$db_port" "$db_name"
fi

echo "✓ PostgreSQL ready (local native, port $db_port). Database: $db_name"
