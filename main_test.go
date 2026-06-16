package main

import (
	"strings"
	"testing"
)

func TestPostgresDSNOmitsEmptyPassword(t *testing.T) {
	t.Setenv("DB_HOST", "/run/postgresql")
	t.Setenv("DB_USER", "octopusdl")
	t.Setenv("DB_NAME", "scheduler")
	t.Setenv("DB_PASSWORD", "")

	dsn := postgresDSN()
	if strings.Contains(dsn, "password=") {
		t.Errorf("expected no password field when DB_PASSWORD is empty, got %q", dsn)
	}
	if !strings.Contains(dsn, "dbname=scheduler") {
		t.Errorf("expected dbname=scheduler, got %q", dsn)
	}
}

func TestPostgresDSNIncludesPasswordWhenSet(t *testing.T) {
	t.Setenv("DB_HOST", "db.example")
	t.Setenv("DB_USER", "scheduler_writer")
	t.Setenv("DB_NAME", "scheduler")
	t.Setenv("DB_PASSWORD", "s3cret")

	dsn := postgresDSN()
	if !strings.Contains(dsn, "password=s3cret") {
		t.Errorf("expected password=s3cret, got %q", dsn)
	}
	if !strings.Contains(dsn, "dbname=scheduler") {
		t.Errorf("expected dbname=scheduler, got %q", dsn)
	}
}
