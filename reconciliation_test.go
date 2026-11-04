package main

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func reconciliationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "usage.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Usage{}, &OctopusAPIResponse{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDailyReconciliationRecoversLateReadingsAndCorrectsExistingRows(t *testing.T) {
	db := reconciliationDB(t)
	t.Setenv("OCTOPUS_API_KEY", "test-key")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	day := time.Date(2026, time.October, 27, 0, 0, 0, 0, time.UTC)
	// Gas and electricity share timestamps but must remain separate rows.
	if err := writeUsageToDb(db, []Usage{{IntervalStart: day, IntervalEnd: day.Add(30 * time.Minute), Consumption: 5}}, "gas"); err != nil {
		t.Fatal(err)
	}
	lateReadingsAvailable := false
	requests := 0
	http.DefaultTransport = testRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Host != "api.octopus.energy" || !strings.Contains(request.URL.Path, "electricity-meter-points") {
			t.Fatalf("unexpected request %s", request.URL)
		}
		from, err := time.Parse(time.RFC3339, request.URL.Query().Get("period_from"))
		if err != nil {
			t.Fatal(err)
		}
		to, err := time.Parse(time.RFC3339, request.URL.Query().Get("period_to"))
		if err != nil {
			t.Fatal(err)
		}
		if !from.Before(to) || to.Sub(from) > 48*time.Hour {
			t.Fatalf("unbounded request %s", request.URL)
		}
		count := 1
		consumption := .125
		if lateReadingsAvailable {
			count, consumption = 48, .25
		}
		readings := []Usage{}
		for i := 0; i < count; i++ {
			start := day.Add(time.Duration(i) * 30 * time.Minute)
			if !start.Before(from) && !start.After(to) {
				readings = append(readings, Usage{IntervalStart: start, IntervalEnd: start.Add(30 * time.Minute), Consumption: consumption})
			}
		}
		body, err := json.Marshal(Response{Count: len(readings), Results: readings})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	if err := downloadRecentUsage(db, "electricity", day.AddDate(0, 0, 2).Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&Usage{}).Where("usage_type = ?", "electricity").Count(&count)
	if count != 1 {
		t.Fatalf("first sparse response stored %d readings, want 1", count)
	}

	// The full day appears more than 48h later. The former daily window would
	// never request this date again, despite the complete data now being available.
	lateReadingsAvailable = true
	now := time.Date(2026, time.November, 4, 4, 0, 0, 0, time.UTC)
	if err := downloadRecentUsage(db, "electricity", now); err != nil {
		t.Fatal(err)
	}
	db.Model(&Usage{}).Where("usage_type = ?", "electricity").Count(&count)
	if count != 48 {
		t.Fatalf("late data did not fill the day: got %d readings", count)
	}
	var stored Usage
	if err := db.Where("interval_start = ? AND usage_type = ?", day, "electricity").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Consumption != .25 {
		t.Fatal("previous partial/corrected value was not updated", stored.Consumption)
	}
	if err := db.Where("interval_start = ? AND usage_type = ?", day, "gas").First(&Usage{}).Error; err != nil {
		t.Fatal("electricity reconciliation removed gas", err)
	}
	if requests != 14 {
		t.Fatalf("two single-fuel runs used %d requests, want 14 bounded requests", requests)
	}
	// A later sparse/empty response must not erase the recovered intervals.
	lateReadingsAvailable = false
	if err := downloadRecentUsage(db, "electricity", now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	db.Model(&Usage{}).Where("usage_type = ?", "electricity").Count(&count)
	if count != 48 {
		t.Fatalf("sparse retry removed recovered data: %d readings remain", count)
	}
	db.Model(&OctopusAPIResponse{}).Count(&count)
	if count != int64(requests) {
		t.Fatalf("responses not all audited: got %d for %d requests", count, requests)
	}
}

func TestReconciliationStopsAndAuditsFailedChunk(t *testing.T) {
	db := reconciliationDB(t)
	t.Setenv("OCTOPUS_API_KEY", "test-key")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	requests := 0
	http.DefaultTransport = testRoundTripper(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
	})
	if err := downloadRecentUsage(db, "electricity", time.Now()); err == nil {
		t.Fatal("failed chunk was reported as success")
	}
	if requests != 1 {
		t.Fatal("continued requesting after failure", requests)
	}
	var audit OctopusAPIResponse
	if err := db.First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.StatusCode != 503 {
		t.Fatal("failed response not audited", audit.StatusCode)
	}
}

func TestReconciliationUsesUTCBoundariesAcrossClockChange(t *testing.T) {
	now, err := time.Parse(time.RFC3339, "2026-10-25T00:30:00+01:00")
	if err != nil {
		t.Fatal(err)
	}
	from, to := recentDownloadWindow(now)
	if to.Format(time.RFC3339) != "2026-10-24T00:00:00Z" || from.Format(time.RFC3339) != "2026-10-10T00:00:00Z" {
		t.Fatalf("unexpected UTC window %s to %s", from, to)
	}
}
