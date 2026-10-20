package main

import (
	"net"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name string
		sig  syscall.Signal
	}{
		{name: "SIGINT", sig: syscall.SIGINT},
		{name: "SIGTERM", sig: syscall.SIGTERM},
		{name: "SIGHUP", sig: syscall.SIGHUP},
		{name: "SIGQUIT", sig: syscall.SIGQUIT},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := freeAddr(t)

			// run starts the child before registering its signal handler, so signals
			// sent earlier would terminate the test binary. signal.Ignore would prevent
			// that but leaves the child with SIG_IGN, so catch them with Notify instead.
			// The loop below keeps signalling until run returns.
			sigs := []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}
			signal.Notify(make(chan os.Signal, 1), sigs...)
			t.Cleanup(func() { signal.Reset(sigs...) })

			done := make(chan int, 1)
			go func() {
				done <- run([]string{"-addr", addr, "--", "/bin/sleep", "60"})
			}()

			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			timeout := time.After(10 * time.Second)
			for {
				select {
				case code := <-done:
					if code != 0 {
						t.Fatalf("run() = %d, want 0", code)
					}
					ln, err := net.Listen("tcp", addr)
					if err != nil {
						t.Fatalf("listener still bound after shutdown: %v", err)
					}
					_ = ln.Close()
					return
				case <-ticker.C:
					_ = syscall.Kill(os.Getpid(), tt.sig)
				case <-timeout:
					t.Fatalf("run() did not return after %s", tt.name)
				}
			}
		})
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}
