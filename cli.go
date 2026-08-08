package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

var errUsage = errors.New("usage")

type config struct {
	addr    string
	timeout time.Duration
	command []string
}

func parseArgs(args []string) (config, error) {
	fs := flag.NewFlagSet("poke", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	addr := fs.String("addr", "127.0.0.1:9999", "HTTP listen address")
	timeout := fs.Duration("timeout", 5*time.Second, "graceful kill wait before SIGKILL")

	if err := fs.Parse(args); err != nil {
		return config{}, fmt.Errorf("%w: %v", errUsage, err)
	}

	command := fs.Args()
	if len(command) == 0 {
		return config{}, fmt.Errorf("%w: missing command", errUsage)
	}

	return config{
		addr:    *addr,
		timeout: *timeout,
		command: command,
	}, nil
}
