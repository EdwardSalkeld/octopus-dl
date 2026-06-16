package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	octopusRequestTimeout = 30 * time.Second
	octopusMaxRetries     = 3
	octopusChunkSize      = 48 * time.Hour
)

// Response matches the overall JSON structure from the API.
type Response struct {
	Count    int     `json:"count"`
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
	Results  []Usage `json:"results"`
}

// Usage matches the structure of a single consumption data point and is a GORM model.
type Usage struct {
	Consumption   float64   `json:"consumption"`
	IntervalStart time.Time `json:"interval_start" gorm:"primaryKey"`
	IntervalEnd   time.Time `json:"interval_end"`
	UsageType     string    `json:"usage_type" gorm:"primaryKey"`
}

func downloadAndStoreUsage(db *gorm.DB, url string, usageType string) error {
	jsonData, err := downloadFromOctopus(url)
	if err != nil {
		return err
	}

	usageData, err := parseOctopusData(jsonData)
	if err != nil {
		return err
	}

	if err := writeUsageToDb(db, usageData.Results, usageType); err != nil {
		return err
	}

	fmt.Printf("Successfully downloaded and processed %s data.\n", usageType)
	return nil
}

func OctopusElectricityTask(db *gorm.DB) error {
	return downloadAndStoreUsage(db, buildOctopusURL(electricityBaseURL()), "electricity")
}

func OctopusGasTask(db *gorm.DB) error {
	return downloadAndStoreUsage(db, buildOctopusURL(gasBaseURL()), "gas")
}

func OctopusBackfillTask(db *gorm.DB, periodFrom, periodTo time.Time, usageType string) error {
	if !periodFrom.Before(periodTo) {
		return errors.New("backfill period_from must be before period_to")
	}

	runTypes := []string{usageType}
	if usageType == "both" {
		runTypes = []string{"electricity", "gas"}
	}

	for _, currentType := range runTypes {
		baseURL, err := octopusBaseURL(currentType)
		if err != nil {
			return err
		}

		for windowStart := periodFrom.UTC(); windowStart.Before(periodTo.UTC()); windowStart = windowStart.Add(octopusChunkSize) {
			windowEnd := windowStart.Add(octopusChunkSize)
			if windowEnd.After(periodTo.UTC()) {
				windowEnd = periodTo.UTC()
			}

			fmt.Printf("Backfilling %s data from %s to %s\n", currentType, windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339))
			if err := downloadAndStoreUsage(db, buildOctopusURLForWindow(baseURL, windowStart, windowEnd), currentType); err != nil {
				return err
			}
		}
	}

	return nil
}

func buildOctopusURL(baseURL string) string {
	now := time.Now().UTC()
	year, month, day := now.Date()
	periodTo := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	periodFrom := periodTo.Add(-octopusChunkSize)

	return buildOctopusURLForWindow(baseURL, periodFrom, periodTo)
}

func buildOctopusURLForWindow(baseURL string, periodFrom, periodTo time.Time) string {
	periodFrom = periodFrom.UTC()
	periodTo = periodTo.UTC()

	layout := "2006-01-02T15:04:05Z"
	return fmt.Sprintf(
		"%s?period_from=%s&period_to=%s",
		baseURL,
		periodFrom.Format(layout),
		periodTo.Format(layout),
	)
}

func downloadFromOctopus(url string) ([]byte, error) {
	apiKey := os.Getenv("OCTOPUS_API_KEY")
	if apiKey == "" {
		return nil, errors.New("OCTOPUS_API_KEY not set in .env file")
	}

	fmt.Printf("Retrieving from %s\n", url)

	client := &http.Client{Timeout: octopusRequestTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.SetBasicAuth(apiKey, "")

	var resp *http.Response
	for attempt := 1; attempt <= octopusMaxRetries; attempt++ {
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		if attempt == octopusMaxRetries {
			return nil, fmt.Errorf("making request: %w", err)
		}
		log.Printf("Octopus request failed on attempt %d/%d: %v", attempt, octopusMaxRetries, err)
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("API request failed with status code %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return body, nil
}

func parseOctopusData(jsonData []byte) (Response, error) {
	var response Response
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return Response{}, fmt.Errorf("parsing JSON: %w", err)
	}
	return response, nil
}

func writeUsageToDb(db *gorm.DB, usages []Usage, usageType string) error {
	if len(usages) == 0 {
		fmt.Println("No new usage data to save.")
		return nil
	}

	for i := range usages {
		usages[i].UsageType = usageType
	}

	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "interval_start"}, {Name: "usage_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"consumption", "interval_end"}),
	}).Create(&usages)

	if result.Error != nil {
		return fmt.Errorf("writing to database: %w", result.Error)
	}

	fmt.Printf("Successfully saved %d usage records to the database.\n", result.RowsAffected)
	return nil
}

func electricityBaseURL() string {
	return "https://api.octopus.energy/v1/electricity-meter-points/2343442199414/meters/24E6010855/consumption/"
}

func gasBaseURL() string {
	return "https://api.octopus.energy/v1/gas-meter-points/1821376409/meters/E6E17082422543/consumption/"
}

func octopusBaseURL(usageType string) (string, error) {
	switch usageType {
	case "electricity":
		return electricityBaseURL(), nil
	case "gas":
		return gasBaseURL(), nil
	default:
		return "", fmt.Errorf("unknown usage type: %s", usageType)
	}
}
