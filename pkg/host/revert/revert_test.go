package revert

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResolveTimeout(t *testing.T) {
	d := func(v time.Duration) *time.Duration { return &v }
	tests := []struct {
		name      string
		flag, key *time.Duration // nil: not set
		want      time.Duration
		wantFrom  string // in the error, when refused
	}{
		{"default", nil, nil, 180 * time.Second, ""},
		{"key", nil, d(300 * time.Second), 300 * time.Second, ""},
		{"flag wins over key", d(120 * time.Second), d(300 * time.Second), 120 * time.Second, ""},
		{"floor is accepted", d(MinTimeout), nil, MinTimeout, ""},
		{"rounded up to a second", nil, d(90*time.Second + time.Millisecond), 91 * time.Second, ""},
		{"flag below the floor", d(18 * time.Second), nil, 0, "--confirm-timeout 18s"},
		{"key below the floor", nil, d(59 * time.Second), 0, "confirmTimeout 59s"},
		{"an explicit 0 flag is refused, not the default", d(0), nil, 0, "--confirm-timeout 0s"},
		{"an explicit 0 key is refused, not the default", nil, d(0), 0, "confirmTimeout 0s"},
		{"negative", d(-time.Second), nil, 0, "--confirm-timeout"},
		{"a low key even with a flag set does not matter", d(200 * time.Second), d(time.Second), 200 * time.Second, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTimeout(tt.flag, tt.key)
			if tt.wantFrom != "" {
				if !errors.Is(err, ErrTimeout) || !strings.Contains(err.Error(), tt.wantFrom) {
					t.Fatalf("ResolveTimeout = %v, %v; want ErrTimeout naming %q", got, err, tt.wantFrom)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("ResolveTimeout = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		bad  bool
	}{
		{"180", 180 * time.Second, false},
		{" 240 ", 240 * time.Second, false},
		{"3m", 3 * time.Minute, false},
		{"90s", 90 * time.Second, false},
		{"0", 0, false},
		{"-5", 0, true},
		{"-1m", 0, true},
		{"soon", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseTimeout(tt.in)
		if tt.bad != (err != nil) || got != tt.want || tt.bad && !errors.Is(err, ErrTimeout) {
			t.Errorf("ParseTimeout(%q) = %v, %v; want %v, error %v", tt.in, got, err, tt.want, tt.bad)
		}
	}
}

func TestValidID(t *testing.T) {
	for id, want := range map[string]bool{
		testID:                  true,
		NewID(testNow):          true,
		"a":                     true,
		"a_b-c":                 true,
		"":                      false,
		"-a":                    false,
		"_a":                    false,
		"a/b":                   false,
		"a.b":                   false,
		"a b":                   false,
		"../x":                  false,
		strings.Repeat("a", 64): true,
		strings.Repeat("a", 65): false,
	} {
		if ValidID(id) != want {
			t.Errorf("ValidID(%q) = %v, want %v", id, !want, want)
		}
	}
}
