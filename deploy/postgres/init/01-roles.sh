#!/bin/sh
# Creates runtime login roles for local development. In production these roles
# are created by the platform team with secrets from the secret manager.
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<SQL
CREATE ROLE app_rw LOGIN PASSWORD '${APP_RW_PASSWORD}' NOSUPERUSER NOBYPASSRLS;
CREATE ROLE app_system LOGIN PASSWORD '${APP_SYSTEM_PASSWORD}' NOSUPERUSER BYPASSRLS;
CREATE DATABASE shopee_automation_test OWNER app_owner;
SQL
