#!/usr/bin/env bash
# Runs ONCE on a fresh postgres data volume (via /docker-entrypoint-initdb.d/).
# Creates the langgraph database alongside the default docreg database so
# LangGraph self-hosted can use the shared postgres container without
# clashing with doc-registry's own schema.
#
# Existing volumes: run once manually with
#   docker exec <pg-container> createdb -U "$POSTGRES_USER" langgraph
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "${POSTGRES_USER}" <<-EOSQL
	SELECT 'CREATE DATABASE langgraph OWNER ${POSTGRES_USER}'
	WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'langgraph')\gexec
EOSQL
