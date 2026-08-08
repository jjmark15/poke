package main

import (
	"errors"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    config
		wantErr error
	}{
		{
			name: "defaults with command",
			args: []string{"--", "echo", "hi"},
			want: config{
				addr:    "127.0.0.1:9999",
				timeout: 5 * time.Second,
				command: []string{"echo", "hi"},
			},
		},
		{
			name: "custom addr and timeout",
			args: []string{"-addr", "127.0.0.1:8080", "-timeout", "2s", "--", "/bin/sleep", "1"},
			want: config{
				addr:    "127.0.0.1:8080",
				timeout: 2 * time.Second,
				command: []string{"/bin/sleep", "1"},
			},
		},
		{
			name:    "missing command",
			args:    []string{},
			wantErr: errUsage,
		},
		{
			name:    "empty after dash dash",
			args:    []string{"--"},
			wantErr: errUsage,
		},
		{
			name:    "flags only no command",
			args:    []string{"-addr", "127.0.0.1:1"},
			wantErr: errUsage,
		},
		{
			name:    "bad timeout",
			args:    []string{"-timeout", "nope", "--", "echo"},
			wantErr: errUsage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseArgs(tt.args)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.addr != tt.want.addr {
				t.Errorf("addr = %q, want %q", got.addr, tt.want.addr)
			}
			if got.timeout != tt.want.timeout {
				t.Errorf("timeout = %v, want %v", got.timeout, tt.want.timeout)
			}
			if len(got.command) != len(tt.want.command) {
				t.Fatalf("command = %v, want %v", got.command, tt.want.command)
			}
			for i := range got.command {
				if got.command[i] != tt.want.command[i] {
					t.Errorf("command[%d] = %q, want %q", i, got.command[i], tt.want.command[i])
				}
			}
		})
	}
}
