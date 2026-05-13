package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

var db *sql.DB

type SensorValue struct {
	Value      *float64 `json:"value"`
	RecordedAt *string  `json:"recorded_at"`
}

type Latest struct {
	Smell       SensorValue `json:"smell"`
	CO2         SensorValue `json:"co2"`
	Temperature SensorValue `json:"temperature"`
	Humidity    SensorValue `json:"humidity"`
}

func latestValue(table string) SensorValue {
	var v float64
	var t time.Time
	err := db.QueryRow("SELECT value, recorded_at FROM "+table+" ORDER BY recorded_at DESC LIMIT 1").Scan(&v, &t)
	if err != nil {
		return SensorValue{}
	}
	s := t.Format("2006/01/02 15:04:05")
	return SensorValue{Value: &v, RecordedAt: &s}
}

func latestHandler(w http.ResponseWriter, r *http.Request) {
	data := Latest{
		Smell:       latestValue("smells"),
		CO2:         latestValue("co2s"),
		Temperature: latestValue("temperatures"),
		Humidity:    latestValue("humidities"),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pass, ok := r.BasicAuth()
		if !ok || pass != os.Getenv("VIEW_PASSWORD") {
			w.Header().Set("WWW-Authenticate", `Basic realm="sensor-realtime-view"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	godotenv.Load()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "sensor:sensor@tcp(localhost:3306)/sensordb?parseTime=true&loc=Asia%2FTokyo"
	}

	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("cannot connect to db:", err)
	}

	if os.Getenv("VIEW_PASSWORD") == "" {
		log.Fatal("VIEW_PASSWORD is required")
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	http.Handle("/api/latest", basicAuth(http.HandlerFunc(latestHandler)))
	http.Handle("/", basicAuth(http.FileServer(http.Dir("./static"))))

	srv := &http.Server{Addr: ":8080"}
	go func() {
		log.Println("listening on :8080")
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("server shutdown")
}
