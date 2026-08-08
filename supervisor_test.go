package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Build helper binaries used by supervisor tests.
	if err := buildTestHelpers(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

var (
	helperIgnoreTERM string
	helperSpawnSleep string
)

func buildTestHelpers() error {
	dir, err := os.MkdirTemp("", "poke-helpers")
	if err != nil {
		return err
	}

	ignoreSrc := filepath.Join(dir, "ignore_term.go")
	if err := os.WriteFile(ignoreSrc, []byte(`package main
import (
	"os"
	"os/signal"
	"syscall"
	"time"
)
func main() {
	ch := make(chan os.Signal, 8)
	signal.Notify(ch, syscall.SIGTERM)
	go func() {
		for range ch {
		}
	}()
	if len(os.Args) > 1 {
		_ = os.WriteFile(os.Args[1], []byte("ready"), 0o644)
	}
	time.Sleep(60 * time.Second)
}
`), 0o644); err != nil {
		return err
	}
	helperIgnoreTERM = filepath.Join(dir, "ignore_term")
	if out, err := exec.Command("go", "build", "-o", helperIgnoreTERM, ignoreSrc).CombinedOutput(); err != nil {
		return errors.New(string(out) + err.Error())
	}

	spawnSrc := filepath.Join(dir, "spawn_sleep.go")
	if err := os.WriteFile(spawnSrc, []byte(`package main
import ("os"; "os/exec"; "syscall"; "time")
func main() {
	cmd := exec.Command("/bin/sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: false}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	time.Sleep(60 * time.Second)
}
`), 0o644); err != nil {
		return err
	}
	helperSpawnSleep = filepath.Join(dir, "spawn_sleep")
	if out, err := exec.Command("go", "build", "-o", helperSpawnSleep, spawnSrc).CombinedOutput(); err != nil {
		return errors.New(string(out) + err.Error())
	}
	return nil
}

func TestSupervisorStartAndExit(t *testing.T) {
	s := newSupervisor([]string{"/bin/sleep", "60"}, 2*time.Second, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := s.PID()
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if s.PID() != 0 {
		t.Fatalf("pid after shutdown = %d, want 0", s.PID())
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child still alive: %v", err)
	}
}

func TestSupervisorRestartChangesPID(t *testing.T) {
	s := newSupervisor([]string{"/bin/sleep", "60"}, 2*time.Second, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid1 := s.PID()
	if err := s.Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	pid2 := s.PID()
	if pid2 <= 0 || pid2 == pid1 {
		t.Fatalf("pid after restart = %d, before = %d", pid2, pid1)
	}
	_ = s.Shutdown()
}

func TestSupervisorRestartWhenDead(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not on PATH")
	}
	s := newSupervisor([]string{truePath}, 2*time.Second, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool { return s.PID() == 0 })
	if err := s.Restart(); err != nil {
		t.Fatalf("Restart when dead: %v", err)
	}
	_ = s.Shutdown()
}

func TestSupervisorStartFailure(t *testing.T) {
	s := newSupervisor([]string{"/nonexistent/poke-binary-xyz"}, 2*time.Second, testLogger(t))
	err := s.Start()
	if err == nil {
		t.Fatal("expected start error")
	}
	if s.PID() != 0 {
		t.Fatalf("pid = %d after failed start", s.PID())
	}
}

func TestSupervisorKillIgnoresTERM(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	s := newSupervisor([]string{helperIgnoreTERM, ready}, 200*time.Millisecond, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	})
	pid := s.PID()
	start := time.Now()
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 150*time.Millisecond {
		t.Fatalf("shutdown too fast (%v); SIGKILL path may not have waited", elapsed)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child still alive: %v", err)
	}
}

func TestSupervisorProcessGroup(t *testing.T) {
	s := newSupervisor([]string{helperSpawnSleep}, 2*time.Second, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := s.PID()
	time.Sleep(100 * time.Millisecond)
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group still alive: %v", err)
	}
}

func TestSupervisorCoalesceRestarts(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	s := newSupervisor([]string{helperIgnoreTERM, ready}, 150*time.Millisecond, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	})

	const n = 5
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			errs[i] = s.Restart()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("restart %d: %v", i, err)
		}
	}
	if s.PID() <= 0 {
		t.Fatal("expected running child after coalesce")
	}
	_ = s.Shutdown()
}

func TestSupervisorNoStartDuringShutdown(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	s := newSupervisor([]string{helperIgnoreTERM, ready}, 2*time.Second, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	})
	done := make(chan error, 1)
	go func() { done <- s.Shutdown() }()
	time.Sleep(50 * time.Millisecond)
	err := s.Restart()
	if !errors.Is(err, errShuttingDown) {
		t.Fatalf("Restart during shutdown: %v, want errShuttingDown", err)
	}
	<-done
}

func TestSupervisorForceKill(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	s := newSupervisor([]string{helperIgnoreTERM, ready}, 10*time.Second, testLogger(t))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	})
	pid := s.PID()
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.ForceKill()
	}()
	start := time.Now()
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("ForceKill did not cut shutdown short")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child still alive: %v", err)
	}
}

func testLogger(t *testing.T) *logger {
	t.Helper()
	return newLogger(os.Stderr)
}

func waitUntil(t *testing.T, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for condition")
}
