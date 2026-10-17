package sudoers_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

const (
	runDir = "/run/hardn/20261017T143005Z-1a2b3c4d"
	work   = runDir + "/" + sudoers.WorkDir
	// debianMain is the shape of Debian 13's /etc/sudoers: the %sudo rule,
	// then the includedir as the last directive.
	debianMain = "Defaults\tenv_reset\nroot\tALL=(ALL:ALL) ALL\n%sudo\tALL=(ALL:ALL) ALL\n\n@includedir /etc/sudoers.d\n"
	// cloudInit is cloud-init's drop-in for its default user.
	cloudInit = "# Created by cloud-init v. 25.1.4\n\n# User rules for bob\nbob ALL=(ALL) NOPASSWD:ALL\n"
	// alpineDoasConf is the stock /etc/doas.conf (its rule commented out);
	// wheel is setup-user's /etc/doas.d/20-wheel.conf.
	alpineDoasConf = "# see doas.conf(5) for configuration details\n\n# permit persist :wheel\n"
	wheel          = "permit persist :wheel\n"
)

// sudoHost is a Debian-like host: /etc/sudoers, /etc/sudoers.d with its
// README and cloud-init's file, and the run directory.
func sudoHost(t *testing.T) *hostfake.System {
	t.Helper()
	h := hostfake.New(host.Options{})
	h.AddFile(sudoers.MainConfig, []byte(debianMain), 0o440, 0, 0)
	h.AddDir(sudoers.DropInDir, 0o755, 0, 0)
	h.AddFile(sudoers.DropInDir+"/README", []byte("# files here are read by sudo\n"), 0o440, 0, 0)
	h.AddFile(sudoers.DropInDir+"/90-cloud-init-users", []byte(cloudInit), 0o440, 0, 0)
	h.AddDir(runDir, 0o700, 0, 0)
	h.Handler = model(h)
	return h
}

// doasHost is an Alpine host after setup-alpine: doas.conf and 20-wheel.conf.
func doasHost(t *testing.T) *hostfake.System {
	t.Helper()
	h := hostfake.New(host.Options{})
	h.AddFile(sudoers.DoasConfig, []byte(alpineDoasConf), 0o640, 0, 0)
	h.AddDir(sudoers.DoasDir, 0o750, 0, 0)
	h.AddFile(sudoers.DoasDir+"/20-wheel.conf", []byte(wheel), 0o644, 0, 0)
	h.AddDir(runDir, 0o700, 0, 0)
	h.Handler = model(h)
	return h
}

// bad marks content the modelled tools reject.
const bad = "BAD"

// model answers visudo and doas as the real tools would for the rules of
// these tests: a file is rejected when it is missing or holds bad.
// visudo -c reads /etc/sudoers and every name sudo reads in /etc/sudoers.d.
// The answers' wording follows sudo's and doas's messages; it was not
// captured from a host.
func model(h *hostfake.System) func([]string, []byte) hostfake.Response {
	rejects := func(p string) bool {
		e, ok := h.Entry(p)
		return !ok || bytes.Contains(e.Data, []byte(bad))
	}
	fail := func(p string) hostfake.Response {
		return hostfake.Response{Stderr: "syntax error in " + p + " near line 2\n", ExitCode: 1}
	}
	return func(argv []string, _ []byte) hostfake.Response {
		switch {
		case slices.Equal(argv, []string{"visudo", "-c"}):
			files := []string{sudoers.MainConfig}
			for _, n := range names(h, sudoers.DropInDir) {
				if !strings.Contains(n, ".") && !strings.HasSuffix(n, "~") {
					files = append(files, sudoers.DropInDir+"/"+n)
				}
			}
			for _, f := range files {
				if rejects(f) {
					return fail(f)
				}
			}
			return hostfake.Response{Stdout: "/etc/sudoers: parsed OK\n"}
		case len(argv) == 3 && argv[0] == "visudo" && argv[1] == "-cf", len(argv) == 3 && argv[0] == "doas" && argv[1] == "-C":
			if rejects(argv[2]) {
				return fail(argv[2])
			}
			if argv[0] == "visudo" {
				return hostfake.Response{Stdout: argv[2] + ": parsed OK\n"}
			}
			return hostfake.Response{}
		}
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
}

// names lists dir in the fake (the listing is recorded as a call).
func names(h *hostfake.System, dir string) []string {
	des, _ := h.ReadDir(context.Background(), dir)
	var out []string
	for _, d := range des {
		out = append(out, d.Name())
	}
	return out
}

// plan reads and plans policies on h.
func plan(t *testing.T, h host.System, c sudoers.Choice, ps ...sudoers.Policy) (sudoers.State, sudoers.Plan) {
	t.Helper()
	var users []string
	for _, p := range ps {
		users = append(users, p.User)
	}
	st, err := sudoers.Read(context.Background(), h, c, users)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	p, err := sudoers.PlanFor(st, ps)
	if err != nil {
		t.Fatalf("PlanFor: %v", err)
	}
	return st, p
}

func runs(h *hostfake.System) []string {
	var out []string
	for _, r := range h.Runs() {
		out = append(out, strings.Join(r, " "))
	}
	return out
}

// writes lists the paths written, removed or created, in order.
func writes(h *hostfake.System) []string {
	var out []string
	for _, c := range h.Calls() {
		switch c.Op {
		case "write-atomic", "write-in-place", "remove", "removeall", "mkdir", "rename", "chmod", "chown":
			out = append(out, c.Op+" "+c.Path)
		}
	}
	return out
}

// checkFile fails unless h holds path with data, mode and root:root.
func checkFile(t *testing.T, h *hostfake.System, p, data string, mode uint32) {
	t.Helper()
	e, ok := h.Entry(p)
	switch {
	case !ok:
		t.Errorf("%s: missing", p)
	case string(e.Data) != data || uint32(e.Mode.Perm()) != mode || e.UID != 0 || e.GID != 0:
		t.Errorf("%s = %q %04o %d:%d, want %q %04o 0:0", p, e.Data, uint32(e.Mode.Perm()), e.UID, e.GID, data, mode)
	}
}

func checkAbsent(t *testing.T, h *hostfake.System, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, ok := h.Entry(p); ok {
			t.Errorf("%s exists", p)
		}
	}
}
