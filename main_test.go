package main

import (
	"net/http"
	"net/http/httptest"
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

func TestManualRunEndpoint(t *testing.T) {
	var calls int
	server := httptest.NewServer(manualRunHandler(func() error {
		calls++
		return nil
	}))
	defer server.Close()

	response, err := http.Post(server.URL+"/run", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /run: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || calls != 1 {
		t.Fatalf("unexpected manual run response: status=%d calls=%d", response.StatusCode, calls)
	}

	response, err = http.Get(server.URL + "/run")
	if err != nil {
		t.Fatalf("GET /run: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /run status=%d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
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
