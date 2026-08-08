package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHealth(t *testing.T) {
	s := newSupervisor([]string{"/bin/sleep", "60"}, 2*time.Second, testLogger(t))
	srv := newServer(s, testLogger(t))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); body != "ok\n" {
		t.Fatalf("body = %q", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestPokeRestarts(t *testing.T) {
	s := newSupervisor([]string{"/bin/sleep", "60"}, 2*time.Second, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Shutdown()
	pid1 := s.PID()

	srv := newServer(s, testLogger(t))
	req := httptest.NewRequest(http.MethodPost, "/poke", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "restarted\n" {
		t.Fatalf("body = %q", body)
	}
	pid2 := s.PID()
	if pid2 <= 0 || pid2 == pid1 {
		t.Fatalf("pid after poke = %d, before = %d", pid2, pid1)
	}
}

func TestPokeStartFailure(t *testing.T) {
	s := newSupervisor([]string{"/nonexistent/poke-binary-xyz"}, 2*time.Second, testLogger(t))
	srv := newServer(s, testLogger(t))
	req := httptest.NewRequest(http.MethodPost, "/poke", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "start failed:") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestPokeShuttingDown(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	s := newSupervisor([]string{helperIgnoreTERM, ready}, 2*time.Second, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	})
	go s.Shutdown()
	time.Sleep(30 * time.Millisecond)

	srv := newServer(s, testLogger(t))
	req := httptest.NewRequest(http.MethodPost, "/poke", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "shutting down\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestNotFound(t *testing.T) {
	s := newSupervisor([]string{"/bin/sleep", "60"}, 2*time.Second, testLogger(t))
	srv := newServer(s, testLogger(t))
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHealthDuringSlowPoke(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	s := newSupervisor([]string{helperIgnoreTERM, ready}, 300*time.Millisecond, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	})
	defer s.Shutdown()

	srv := newServer(s, testLogger(t))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/poke", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
	}()
	time.Sleep(30 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	start := time.Now()
	srv.ServeHTTP(rec, req)
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("health blocked during poke")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	wg.Wait()
}
