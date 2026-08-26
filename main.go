package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	backfillFrom := flag.String("backfill-from", "", "UTC start timestamp for Octopus backfill, e.g. 2026-04-10T00:00:00Z.")
	backfillTo := flag.String("backfill-to", "", "UTC end timestamp for Octopus backfill, e.g. 2026-04-12T00:00:00Z.")
	backfillType := flag.String("backfill-type", "both", "Octopus usage type to backfill: electricity, gas, or both.")
	listenAddr := flag.String("listen-addr", "", "serve a manual-run HTTP endpoint at this address")
	flag.Parse()

	// .env is optional: the systemd unit supplies configuration via the
	// environment, so a missing file is not fatal.
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file loaded (%v); relying on the environment.", err)
	}

	db := initDB()
	if *listenAddr != "" {
		if *backfillFrom != "" || *backfillTo != "" {
			log.Fatal("-listen-addr cannot be combined with backfill options")
		}
		log.Fatal(serveManualRunEndpoint(*listenAddr, func() error {
			return runDailyDownload(db)
		}))
	}

	if *backfillFrom != "" || *backfillTo != "" {
		runBackfill(db, *backfillFrom, *backfillTo, *backfillType)
		return
	}

	if err := runDailyDownload(db); err != nil {
		log.Fatal(err)
	}
}

func runDailyDownload(db *gorm.DB) error {
	if err := OctopusElectricityTask(db); err != nil {
		return fmt.Errorf("Octopus electricity download failed: %w", err)
	}
	if err := OctopusGasTask(db); err != nil {
		return fmt.Errorf("Octopus gas download failed: %w", err)
	}
	return nil
}

func serveManualRunEndpoint(listenAddr string, run func() error) error {
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           manualRunHandler(run),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Serving manual Octopus download endpoint on %s", listenAddr)
	return server.ListenAndServe()
}

func manualRunHandler(run func() error) http.Handler {
	var running atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /run", func(w http.ResponseWriter, _ *http.Request) {
		if !running.CompareAndSwap(false, true) {
			http.Error(w, "an Octopus download is already running", http.StatusConflict)
			return
		}
		defer running.Store(false)

		if err := run(); err != nil {
			log.Printf("Manual Octopus download failed: %v", err)
			http.Error(w, "Octopus download failed", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
	})

	return mux
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
