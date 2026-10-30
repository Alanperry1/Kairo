#!/usr/bin/env python3
"""Export a consistent, read-only SQLite snapshot as atomic PostgreSQL import SQL.

Pipe stdout to psql with ON_ERROR_STOP=1. The target must be empty. Artifact
files remain in DATA_DIR; this script migrates metadata, not those files.
"""
import argparse
from pathlib import Path
import sqlite3
import sys

TABLES = {
    "projects": ("id", "payload"),
    "datasets": ("id", "payload"),
    "experiments": ("id", "status", "updated_at", "payload"),
    "plan_versions": ("experiment_id", "version", "payload"),
    "events": ("id", "experiment_id", "kind", "message", "created_at"),
    "artifacts": ("experiment_id", "name", "payload"),
}

def literal(value):
    if isinstance(value, bytes):
        return "decode('" + value.hex() + "','hex')"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, str) and "\x00" not in value:
        return "'" + value.replace("'", "''") + "'"
    raise ValueError("Unsupported SQLite value; import aborted")

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    args = parser.parse_args()
    source = args.source.resolve(strict=True)
    connection = sqlite3.connect(source.as_uri() + "?mode=ro", uri=True)
    connection.execute("BEGIN")
    # Validate and snapshot all tables before producing executable output.
    tables = {name: connection.execute("SELECT " + ",".join(columns) + " FROM " + name).fetchall() for name, columns in TABLES.items()}
    connection.close()
    statements = ["BEGIN;", "SET LOCAL standard_conforming_strings = on;", "SELECT pg_advisory_xact_lock(728104923);", Path(__file__).resolve().parents[1].joinpath("internal/research/schema.sql").read_text(), "LOCK TABLE " + ",".join(TABLES) + " IN ACCESS EXCLUSIVE MODE;"]
    check = " OR ".join("EXISTS(SELECT 1 FROM " + table + ")" for table in TABLES)
    statements.append("DO $$ BEGIN IF " + check + " THEN RAISE EXCEPTION 'Target database must be empty; no records were imported'; END IF; END $$;")
    for name, rows in tables.items():
        for row in rows:
            statements.append("INSERT INTO " + name + "(" + ",".join(TABLES[name]) + ") VALUES(" + ",".join(literal(value) for value in row) + ");")
    statements.append("SELECT setval(pg_get_serial_sequence('events','id'), COALESCE(MAX(id),1), MAX(id) IS NOT NULL) FROM events;")
    statements.append("COMMIT;")
    sys.stdout.write("\n".join(statements) + "\n")
    print("Exported SQLite snapshot: " + ", ".join(str(len(rows)) + " " + name for name, rows in tables.items()), file=sys.stderr)

if __name__ == "__main__":
    main()
