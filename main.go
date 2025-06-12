package main

import (
	"log"
	"net/http"

	"github.com/steveoni/microvm/api"
	"github.com/steveoni/microvm/db"
	"github.com/steveoni/microvm/jobs"
)

func main() {
	if err := db.InitDB("jobs.db"); err != nil {
		log.Fatal("DB init failed:", err)
	}

	if err := jobs.InitClient("localhost:6379"); err != nil {
		log.Fatal("Redis failed:", err)
	}

	go func() {
		s := jobs.NewServer("localhost:6379")
		if err := s.Run(jobs.Handler()); err != nil {
			log.Fatal("Worker failed:", err)
		}
	}()

	log.Println("API running on :8080")
	http.ListenAndServe(":8080", api.NewRouter())
}
