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
	server := httptest.NewServer(manualRunHandler(func(backfill *manualBackfillRequest) error {
		if backfill != nil {
			t.Fatalf("expected daily download, got backfill %#v", backfill)
		}
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

func TestManualRunEndpointBackfill(t *testing.T) {
	var received *manualBackfillRequest
	server := httptest.NewServer(manualRunHandler(func(backfill *manualBackfillRequest) error {
		received = backfill
		return nil
	}))
	defer server.Close()

	response, err := http.Post(server.URL+"/run", "application/json", strings.NewReader(`{"period_from":"2026-08-24T00:00:00Z","period_to":"2026-08-31T00:00:00Z","usage_type":"GAS"}`))
	if err != nil {
		t.Fatalf("POST /run: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /run status=%d, want %d", response.StatusCode, http.StatusOK)
	}
	if received == nil || received.PeriodFrom != "2026-08-24T00:00:00Z" || received.PeriodTo != "2026-08-31T00:00:00Z" || received.UsageType != "gas" {
		t.Fatalf("unexpected backfill request: %#v", received)
	}
}

func TestManualRunEndpointRejectsInvalidBackfill(t *testing.T) {
	called := false
	server := httptest.NewServer(manualRunHandler(func(*manualBackfillRequest) error {
		called = true
		return nil
	}))
	defer server.Close()

	response, err := http.Post(server.URL+"/run", "application/json", strings.NewReader(`{"period_from":"2026-08-31T00:00:00Z","period_to":"2026-08-24T00:00:00Z"}`))
	if err != nil {
		t.Fatalf("POST /run: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || called {
		t.Fatalf("unexpected invalid backfill response: status=%d called=%t", response.StatusCode, called)
	}
}

func TestManualRunEndpointRejectsUnknownFields(t *testing.T) {
	called := false
	server := httptest.NewServer(manualRunHandler(func(*manualBackfillRequest) error {
		called = true
		return nil
	}))
	defer server.Close()

	response, err := http.Post(server.URL+"/run", "application/json", strings.NewReader(`{"unknown":true}`))
	if err != nil {
		t.Fatalf("POST /run: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || called {
		t.Fatalf("unexpected unknown-field response: status=%d called=%t", response.StatusCode, called)
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
