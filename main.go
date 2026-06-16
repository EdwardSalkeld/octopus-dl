package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	backfillFrom := flag.String("backfill-from", "", "UTC start timestamp for Octopus backfill, e.g. 2026-04-10T00:00:00Z.")
	backfillTo := flag.String("backfill-to", "", "UTC end timestamp for Octopus backfill, e.g. 2026-04-12T00:00:00Z.")
	backfillType := flag.String("backfill-type", "both", "Octopus usage type to backfill: electricity, gas, or both.")
	flag.Parse()

	// .env is optional: the systemd unit supplies configuration via the
	// environment, so a missing file is not fatal.
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file loaded (%v); relying on the environment.", err)
	}

	db := initDB()

	if *backfillFrom != "" || *backfillTo != "" {
		runBackfill(db, *backfillFrom, *backfillTo, *backfillType)
		return
	}

	if err := OctopusElectricityTask(db); err != nil {
		log.Fatalf("Octopus electricity download failed: %v", err)
	}
	if err := OctopusGasTask(db); err != nil {
		log.Fatalf("Octopus gas download failed: %v", err)
	}
}

func runBackfill(db *gorm.DB, backfillFrom, backfillTo, backfillType string) {
	if backfillFrom == "" || backfillTo == "" {
		log.Fatal("Both -backfill-from and -backfill-to must be provided")
	}

	periodFrom, err := time.Parse(time.RFC3339, backfillFrom)
	if err != nil {
		log.Fatalf("Invalid -backfill-from value: %v", err)
	}
	periodTo, err := time.Parse(time.RFC3339, backfillTo)
	if err != nil {
		log.Fatalf("Invalid -backfill-to value: %v", err)
	}

	usageType := strings.ToLower(backfillType)
	fmt.Printf("Running Octopus backfill for %s from %s to %s\n", usageType, periodFrom.Format(time.RFC3339), periodTo.Format(time.RFC3339))
	if err := OctopusBackfillTask(db, periodFrom, periodTo, usageType); err != nil {
		log.Fatalf("Octopus backfill failed: %v", err)
	}
}

func initDB() *gorm.DB {
	db, err := gorm.Open(postgres.Open(postgresDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	return db
}

func postgresDSN() string {
	parts := []string{
		"host=" + envDefault("DB_HOST", "localhost"),
		"port=" + envDefault("DB_PORT", "5432"),
		"user=" + os.Getenv("DB_USER"),
		"dbname=" + os.Getenv("DB_NAME"),
		"sslmode=" + envDefault("DB_SSLMODE", "disable"),
		"TimeZone=UTC",
	}

	// Only include the password when set. An empty "password=" in a
	// keyword/value DSN makes the parser consume the following key (dbname)
	// as the password value, leaving the database name unset. Peer auth over
	// the local socket supplies no password.
	if password := os.Getenv("DB_PASSWORD"); password != "" {
		parts = append(parts, "password="+password)
	}

	return strings.Join(parts, " ")
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
