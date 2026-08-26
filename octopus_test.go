package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBuildOctopusURL(t *testing.T) {
	baseURL := "https://api.octopus.energy/v1/electricity-meter-points/2343442199414/meters/24E6010855/consumption/"
	url := buildOctopusURL(baseURL)

	if !strings.HasPrefix(url, baseURL) {
		t.Errorf("URL prefix is incorrect")
	}

	if !strings.Contains(url, "period_from=") {
		t.Errorf("URL does not contain period_from")
	}

	if !strings.Contains(url, "period_to=") {
		t.Errorf("URL does not contain period_to")
	}
}

func TestBuildOctopusURLForWindow(t *testing.T) {
	baseURL := "https://api.octopus.energy/v1/electricity-meter-points/2343442199414/meters/24E6010855/consumption/"
	periodFrom := time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC)
	periodTo := periodFrom.Add(48 * time.Hour)

	url := buildOctopusURLForWindow(baseURL, periodFrom, periodTo)
	expected := baseURL + "?period_from=2026-04-10T00:00:00Z&period_to=2026-04-12T00:00:00Z"

	if url != expected {
		t.Fatalf("Expected %s, got %s", expected, url)
	}
}

func TestParseOctopusData(t *testing.T) {
	jsonData := `{"count":2,"next":null,"previous":null,"results":[{"consumption":0.123,"interval_start":"2025-09-16T01:00:00+01:00","interval_end":"2025-09-16T01:30:00+01:00"},{"consumption":0.119,"interval_start":"2025-09-16T00:30:00+01:00","interval_end":"2025-09-16T01:00:00+01:00"}]}`

	data, err := parseOctopusData([]byte(jsonData))
	if err != nil {
		t.Fatalf("parseOctopusData returned error: %v", err)
	}

	if data.Count != 2 {
		t.Errorf("Expected count to be 2, but got %d", data.Count)
	}

	if len(data.Results) != 2 {
		t.Errorf("Expected 2 results, but got %d", len(data.Results))
	}

	if data.Results[0].Consumption != 0.123 {
		t.Errorf("Expected first result consumption to be 0.123, but got %f", data.Results[0].Consumption)
	}

	if reflect.TypeOf(data.Results[0].IntervalStart).Kind() != reflect.Struct {
		t.Errorf("Expected IntervalStart to be a time.Time struct, but got %s", reflect.TypeOf(data.Results[0].IntervalStart).Kind())
	}

	expectedTime, _ := time.Parse(time.RFC3339, "2025-09-16T01:00:00+01:00")
	if !data.Results[0].IntervalStart.Equal(expectedTime) {
		t.Errorf("Expected IntervalStart to be %v, but got %v", expectedTime, data.Results[0].IntervalStart)
	}
}

func TestWriteUsageToDb_OnConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	db.AutoMigrate(&Usage{})

	usage1 := []Usage{{
		Consumption:   0.123,
		IntervalStart: time.Now(),
		IntervalEnd:   time.Now().Add(30 * time.Minute),
	}}

	if err := writeUsageToDb(db, usage1, "electricity"); err != nil {
		t.Fatalf("writeUsageToDb returned error: %v", err)
	}

	usage2 := []Usage{{
		Consumption:   0.456,
		IntervalStart: usage1[0].IntervalStart,
		IntervalEnd:   usage1[0].IntervalEnd,
	}}

	if err := writeUsageToDb(db, usage2, "electricity"); err != nil {
		t.Fatalf("writeUsageToDb returned error: %v", err)
	}

	var usages []Usage
	db.Find(&usages)
	if len(usages) != 1 {
		t.Errorf("Expected 1 usage record, but got %d", len(usages))
	}

	if usages[0].Consumption != 0.456 {
		t.Errorf("Expected consumption to be 0.456, but got %f", usages[0].Consumption)
	}

	usage3 := []Usage{{
		Consumption:   0.789,
		IntervalStart: usage1[0].IntervalStart,
		IntervalEnd:   usage1[0].IntervalEnd,
	}}

	if err := writeUsageToDb(db, usage3, "gas"); err != nil {
		t.Fatalf("writeUsageToDb returned error: %v", err)
	}

	db.Find(&usages)
	if len(usages) != 2 {
		t.Errorf("Expected 2 usage records, but got %d", len(usages))
	}
}

func TestWriteAPIResponseToDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	db.AutoMigrate(&OctopusAPIResponse{})

	response := &octopusHTTPResponse{
		statusCode: 200,
		body:       []byte(`{"count":0,"next":null,"previous":null,"results":[]}`),
	}
	if err := writeAPIResponseToDB(db, "electricity", "https://example.test/consumption/", response); err != nil {
		t.Fatalf("writeAPIResponseToDB returned error: %v", err)
	}

	var audit OctopusAPIResponse
	if err := db.First(&audit).Error; err != nil {
		t.Fatalf("reading API response audit: %v", err)
	}
	if audit.StatusCode != 200 || audit.Body != string(response.body) || audit.BodySHA256 == "" {
		t.Errorf("unexpected response audit: %#v", audit)
	}
}
