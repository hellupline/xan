package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/mux"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"gopkg.in/go-playground/validator.v9"
)

var validate = validator.New()

var config struct {
	Address      string
	Port         uint
	GracefulWait time.Duration

	DatabaseDriver string
	DatabseURI     string
}

func main() {
	flag.StringVar(&config.Address, "bind-address", "0.0.0.0", "the address the server will use to bind")
	flag.UintVar(&config.Port, "bind-port", 8080, "the port the server will use to bind")
	flag.DurationVar(&config.GracefulWait, "graceful-timeout", time.Second*15, "the duration for which the server gracefully wait for existing connections to finish - e.g. 15s or 1m")

	flag.StringVar(&config.DatabaseDriver, "database-driver", "sqlite3", "the database driver to use")
	flag.StringVar(&config.DatabseURI, "database-uri", "./xan.db", "the database uri to connect")
	flag.Parse()

	db, err := gorm.Open(config.DatabaseDriver, config.DatabseURI)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.AutoMigrate(&Stats{}).Error; err != nil {
		log.Fatal(err)
	}

	router := mux.NewRouter()
	router.Use(loggingMiddleware)

	router.Methods("POST").Path("/report").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var stats Stats
		if err := json.NewDecoder(r.Body).Decode(&stats); err != nil {
			log.Println("error decoding body")
			io.WriteString(w, err.Error())
			return
		}
		if err := validate.Struct(stats); err != nil {
			log.Println("error validating data")
			io.WriteString(w, err.Error())
			return
		}
		if err := db.Create(&stats).Error; err != nil {
			log.Println("error saving data")
			io.WriteString(w, err.Error())
			return
		}
	})

	srv := newServer(router, fmt.Sprintf("%s:%d", config.Address, config.Port))
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	gracefulTermination(srv, config.GracefulWait)
}

type Stats struct {
	gorm.Model

	AppName     string `validate:"required" json:"app_name" `
	JobName     string `validate:"required" json:"job_name" `
	Version     string `validate:"required" json:"version" `
	BuildNumber string `validate:"required" json:"build_number" `
}

func newServer(router *mux.Router, addr string) *http.Server {
	return &http.Server{
		// Good practice to set timeouts to avoid Slowloris attacks.
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,

		Addr:    addr,
		Handler: router, // Pass our instance of gorilla/mux in.
	}
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
