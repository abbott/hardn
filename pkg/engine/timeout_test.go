package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host/revert"
)

func TestConfirmTimeout(t *testing.T) {
	d := func(v time.Duration) *time.Duration { return &v }
	for _, tc := range []struct {
		name string
		flag *time.Duration
		key  string
		want time.Duration
		err  bool
	}{
		{"the default", nil, "", revert.DefaultTimeout, false},
		{"the key in seconds", nil, "240", 240 * time.Second, false},
		{"the key as a duration", nil, "5m", 5 * time.Minute, false},
		{"the flag wins", d(90 * time.Second), "5m", 90 * time.Second, false},
		{"a key below the floor", nil, "30s", 0, true},
		{"a key that is not one", nil, "soon", 0, true},
		{"a flag below the floor", d(10 * time.Second), "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConfirmTimeout(tc.flag, &config.Config{ConfirmTimeout: tc.key})
			if tc.err != (err != nil) || err != nil && !errors.Is(err, revert.ErrTimeout) || got != tc.want {
				t.Errorf("ConfirmTimeout = %v, %v; want %v (error %v)", got, err, tc.want, tc.err)
			}
		})
	}
	if got, err := ConfirmTimeout(nil, nil); err != nil || got != revert.DefaultTimeout {
		t.Errorf("no configuration: %v, %v", got, err)
	}
}
