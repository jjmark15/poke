package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cfg, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poke: %v\n", err)
		fmt.Fprintf(os.Stderr, "usage: poke [flags] -- <command> [args...]\n")
		return 2
	}

	log := newLogger(os.Stderr)
	sup := newSupervisor(cfg.command, cfg.timeout, log)

	ln, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poke: listen %s: %v\n", cfg.addr, err)
		return 1
	}
	log.printf("listening on %s", cfg.addr)

	srv := &http.Server{Handler: newServer(sup, log)}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.printf("http server error: %v", err)
		}
	}()

	if err := sup.Start(); err != nil {
		log.printf("initial start failed: %v", err)
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.printf("received %s", sig)
	go func() {
		<-sigCh
		log.printf("received second signal")
		sup.ForceKill()
	}()

	_ = srv.Close()
	_ = sup.Shutdown()
	return 0
}
