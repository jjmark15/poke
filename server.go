package main

import (
	"errors"
	"fmt"
	"net/http"
)

type server struct {
	sup *supervisor
	log *logger
	mux *http.ServeMux
}

func newServer(sup *supervisor, log *logger) *server {
	s := &server{sup: sup, log: log, mux: http.NewServeMux()}
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/poke", s.handlePoke)
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed\n", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok\n")
}

func (s *server) handlePoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed\n", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if s.sup.ShuttingDown() {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "shutting down\n")
		return
	}

	s.log.printf("poke received")
	err := s.sup.Restart()
	if err != nil {
		if errors.Is(err, errShuttingDown) {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "shutting down\n")
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "start failed: %v\n", err)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "restarted\n")
}
