package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/mux"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

var config struct {
	Address      string
	Port         uint
	GracefulWait time.Duration
}

func main() {
	flag.StringVar(&config.Address, "bind-address", "0.0.0.0", "the address the server will use to bind")
	flag.UintVar(&config.Port, "bind-port", 8080, "the port the server will use to bind")
	flag.DurationVar(&config.GracefulWait, "graceful-timeout", time.Second*15, "the duration for which the server gracefully wait for existing connections to finish - e.g. 15s or 1m")
	flag.Parse()

	db, _ := gorm.Open("sqlite3", "./xan.db")
	defer db.Close()
	db.AutoMigrate(&Stats{})
	router := mux.NewRouter()
	router.Use(loggingMiddleware)
	router.Methods("POST").Path("/report").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var stats Stats
		json.NewDecoder(r.Body).Decode(&stats)
		defer r.Body.Close()
		db.Create(&stats)
	})

	srv := &http.Server{
		// Good practice to set timeouts to avoid Slowloris attacks.
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,

		Addr:    fmt.Sprintf("%s:%d", config.Address, config.Port),
		Handler: router, // Pass our instance of gorilla/mux in.
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	gracefulTermination(srv, config.GracefulWait)
}

type Stats struct {
	gorm.Model

	AppName     string `json:"app_name"`
	JogName     string `json:"job_name"`
	Version     string `json:"version"`
	BuildNumber string `json:"build_number"`
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println(r.RequestURI)
		next.ServeHTTP(w, r)
	})
}

func gracefulTermination(srv *http.Server, wait time.Duration) {
	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	// SIGKILL, SIGQUIT or SIGTERM (Ctrl+/) will not be caught.
	signal.Notify(c, os.Interrupt)

	// Block until we receive our signal.
	<-c

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	// Doesn't block if no connections, but will otherwise wait
	// until the timeout deadline.
	srv.Shutdown(ctx)
	// Optionally, you could run srv.Shutdown in a goroutine and block on
	// <-ctx.Done() if your application should wait for other services
	// to finalize based on context cancellation.
	log.Println("shutting down")
	os.Exit(0)
}
