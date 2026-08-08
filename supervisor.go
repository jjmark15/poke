package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

var errShuttingDown = errors.New("shutting down")

type logger struct {
	out *os.File
	mu  sync.Mutex
}

func newLogger(out *os.File) *logger {
	return &logger{out: out}
}

func (l *logger) printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "poke: "+format+"\n", args...)
}

type supervisor struct {
	command []string
	timeout time.Duration
	log     *logger

	mu       sync.Mutex
	cmd      *exec.Cmd
	done     chan struct{} // closed when Wait returns
	busy     bool
	pending  bool
	waiters  []chan error
	shutdown bool
	forceCh  chan struct{}
}

func newSupervisor(command []string, timeout time.Duration, log *logger) *supervisor {
	return &supervisor{
		command: command,
		timeout: timeout,
		log:     log,
		forceCh: make(chan struct{}, 1),
	}
}

func (s *supervisor) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.runningLocked() {
		return 0
	}
	return s.cmd.Process.Pid
}

func (s *supervisor) runningLocked() bool {
	if s.cmd == nil || s.cmd.Process == nil || s.done == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *supervisor) Start() error {
	return s.Restart()
}

func (s *supervisor) Restart() error {
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return errShuttingDown
	}
	if s.busy {
		s.pending = true
		ch := make(chan error, 1)
		s.waiters = append(s.waiters, ch)
		s.mu.Unlock()
		return <-ch
	}
	s.busy = true
	s.mu.Unlock()

	var finalErr error
	for {
		finalErr = s.killAndStart()

		s.mu.Lock()
		if s.shutdown {
			s.busy = false
			s.pending = false
			waiters := s.waiters
			s.waiters = nil
			s.mu.Unlock()
			for _, w := range waiters {
				w <- errShuttingDown
			}
			if finalErr == nil {
				finalErr = errShuttingDown
			}
			return errShuttingDown
		}
		if !s.pending {
			s.busy = false
			waiters := s.waiters
			s.waiters = nil
			s.mu.Unlock()
			for _, w := range waiters {
				w <- finalErr
			}
			return finalErr
		}
		s.pending = false
		s.mu.Unlock()
	}
}

func (s *supervisor) killAndStart() error {
	s.mu.Lock()
	cmd := s.cmd
	done := s.done
	running := s.runningLocked()
	s.mu.Unlock()

	if running {
		s.kill(cmd, done)
	}

	s.mu.Lock()
	s.cmd = nil
	s.done = nil
	if s.shutdown {
		s.mu.Unlock()
		return errShuttingDown
	}
	s.mu.Unlock()

	return s.start()
}

func (s *supervisor) start() error {
	if len(s.command) == 0 {
		return fmt.Errorf("empty command")
	}
	path, err := exec.LookPath(s.command[0])
	if err != nil {
		s.log.printf("start failed: %v", err)
		return err
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		s.log.printf("start failed: %v", err)
		return err
	}

	cmd := exec.Command(path, s.command[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		devNull.Close()
		s.log.printf("start failed: %v", err)
		return err
	}
	devNull.Close()

	done := make(chan struct{})
	go func() {
		err := cmd.Wait()
		close(done)
		s.onExit(cmd, err)
	}()

	s.mu.Lock()
	s.cmd = cmd
	s.done = done
	s.mu.Unlock()

	s.log.printf("started pid=%d cmd=%v", cmd.Process.Pid, s.command)
	return nil
}

func (s *supervisor) onExit(cmd *exec.Cmd, err error) {
	s.mu.Lock()
	current := s.cmd == cmd
	s.mu.Unlock()
	if !current {
		return
	}
	if err == nil {
		s.log.printf("child exited code=0")
		return
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		if status, ok := ee.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				s.log.printf("child exited signal=%s", status.Signal())
				return
			}
			s.log.printf("child exited code=%d", status.ExitStatus())
			return
		}
	}
	s.log.printf("child exited: %v", err)
}

func (s *supervisor) kill(cmd *exec.Cmd, done chan struct{}) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid

	// Already dead?
	select {
	case <-done:
		return
	default:
	}

	s.log.printf("sending SIGTERM to pgid=%d", pid)
	_ = syscall.Kill(-pid, syscall.SIGTERM)

	timer := time.NewTimer(s.timeout)
	defer timer.Stop()

	select {
	case <-done:
		return
	case <-timer.C:
		s.log.printf("sending SIGKILL to pgid=%d", pid)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-done
	case <-s.forceCh:
		s.log.printf("sending SIGKILL to pgid=%d", pid)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-done
	}
}

func (s *supervisor) Shutdown() error {
	s.mu.Lock()
	s.shutdown = true
	s.log.printf("shutting down")
	cmd := s.cmd
	done := s.done
	running := s.runningLocked()
	s.mu.Unlock()

	if running {
		s.kill(cmd, done)
	}

	s.mu.Lock()
	s.cmd = nil
	s.done = nil
	s.mu.Unlock()
	return nil
}

func (s *supervisor) ForceKill() {
	select {
	case s.forceCh <- struct{}{}:
	default:
	}
	s.mu.Lock()
	if s.runningLocked() {
		pid := s.cmd.Process.Pid
		s.log.printf("sending SIGKILL to pgid=%d", pid)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	s.mu.Unlock()
}

func (s *supervisor) ShuttingDown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdown
}
