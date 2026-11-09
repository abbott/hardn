package sshd_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// stockMain is shaped like the stock sshd_config of Debian, Ubuntu and
// Proxmox VE (the Include is the first directive), with a Match block.
const stockMain = "# synthetic sshd_config\n\n" + sshd.IncludeLine + "\n\nKbdInteractiveAuthentication no\nSubsystem sftp internal-sftp\n\nMatch User bob\n  PasswordAuthentication yes\n"

const candidate = "# hardn\nPort 2230\nPermitRootLogin prohibit-password\nPasswordAuthentication no\n"

var debian = detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported}

// runDir is the run directory of every fake host: a path in the fake only,
// so the suite does not depend on the real TMPDIR (Validate refuses a run
// directory with a space in it).
const runDir = "/run/hardn/test-1"

// fakeHost is a fake Debian host with stockMain, cloud-init's drop-in, a
// leftover atomic-write temporary file, /run/sshd and a run directory.
type fakeHost struct {
	*hostfake.System
	run, cfg, work string
	seen           map[string]string // the copy as sshd -t saw it
	from           int               // the first call Validate made
}

func newHost(t *testing.T, main string) *fakeHost {
	t.Helper()
	h := &fakeHost{System: hostfake.New(host.Options{}), run: runDir}
	h.work = h.run + "/" + sshd.WorkDir
	h.cfg = h.work + "/sshd_config"
	h.AddDir(h.run, 0o700, 0, 0)
	h.AddFile(sshd.MainConfig, []byte(main), 0o644, 0, 0)
	h.AddFile(sshd.DropInDir+"/50-cloud-init.conf", []byte("PasswordAuthentication yes\n"), 0o600, 0, 0)
	h.AddFile(sshd.DropInDir+"/.00-hardn.conf.hardn-1a2b3c", []byte("Port 22\n"), 0o600, 0, 0)
	h.AddFile(sshd.DropInDir+"/README", []byte("not a drop-in\n"), 0o644, 0, 0)
	h.AddDir(sshd.PrivsepDir, 0o755, 0, 0)
	return h
}

// script answers sshd -t with r (recording the copy) and sshd -T with
// effective; probes are scripted by the test.
func (h *fakeHost) script(r hostfake.Response, effective string) {
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		if !slices.Equal(argv, []string{"sshd", "-t", "-f", h.cfg}) {
			return hostfake.Response{Err: hostfake.ErrUnscripted}
		}
		h.seen = map[string]string{}
		for _, p := range []string{h.cfg, h.work + "/sshd_config.d/00-hardn.conf", h.work + "/sshd_config.d/50-cloud-init.conf", h.work + "/sshd_config.d/.00-hardn.conf.hardn-1a2b3c"} {
			if e, ok := h.Entry(p); ok {
				h.seen[p] = string(e.Data)
			}
		}
		return r
	}
	h.Respond(hostfake.Response{Stdout: effective}, "sshd", "-T", "-f", h.cfg)
}

func (h *fakeHost) request(changes ...sshd.Change) sshd.Request {
	h.from = len(h.Calls())
	return sshd.Request{Changes: changes, RunDir: h.run, Platform: debian}
}

// checkClean fails when the copy is left behind, or anything outside the run
// directory was written.
func (h *fakeHost) checkClean(t *testing.T) {
	t.Helper()
	if _, ok := h.Entry(h.work); ok {
		t.Errorf("%s left behind", h.work)
	}
	for _, c := range h.Calls()[h.from:] {
		switch c.Op {
		case "write-atomic", "write-in-place", "mkdir", "remove", "removeall", "rename", "chmod", "chown":
			if !strings.HasPrefix(c.Path, h.work) {
				t.Errorf("%s outside the copy", c)
			}
		}
	}
}

// redhatCrypto is Rocky Linux's and AlmaLinux's drop-in that includes the
// crypto policy (openssh-server, rocky10-test-110).
const redhatCrypto = sshd.DropInDir + "/40-redhat-crypto-policies.conf"

// policyText is rocky10-test-110's DEFAULT policy, its lists cut short.
const policyText = "Ciphers aes256-gcm@openssh.com,aes256-ctr\nMACs hmac-sha2-256-etm@openssh.com\n" +
	"GSSAPIKexAlgorithms gss-curve25519-sha256-\nKexAlgorithms mlkem768x25519-sha256,curve25519-sha256\n" +
	"HostKeyAlgorithms ssh-ed25519,rsa-sha2-256\nPubkeyAcceptedAlgorithms ssh-ed25519,rsa-sha2-256\n" +
	"HostbasedAcceptedAlgorithms ssh-ed25519\nCASignatureAlgorithms ssh-ed25519\nRequiredRSASize 2048\n"

// cryptoPolicy installs Rocky Linux's drop-in and its policy as there: a
// link into /usr/share/crypto-policies, holding text.
func cryptoPolicy(h *fakeHost, text string) {
	h.AddFile(redhatCrypto, []byte("# crypto policy\n"+sshd.CryptoPolicyInclude+"\n"), 0o600, 0, 0)
	h.AddFile("/usr/share/crypto-policies/DEFAULT/opensshserver.txt", []byte(text), 0o644, 0, 0)
	h.AddSymlink(sshd.CryptoPolicyFile, "/usr/share/crypto-policies/DEFAULT/opensshserver.txt")
}

// TestValidateCryptoPolicy: Rocky Linux's drop-in that includes the crypto
// policy is copied as it is, so sshd reads the policy by its absolute path,
// and the policy is a file of the check (Files); the FIPS policy's
// GSSAPIKeyExchange is a crypto setting too.
func TestValidateCryptoPolicy(t *testing.T) {
	for _, text := range []string{policyText, "GSSAPIKeyExchange no\nCiphers aes256-gcm@openssh.com\n"} {
		h := newHost(t, stockMain)
		cryptoPolicy(h, text)
		h.script(hostfake.Response{}, "port 2230\n")
		h.Respond(hostfake.Response{Stdout: "passwordauthentication yes\n"}, "sshd", "-T", "-f", h.cfg, "-C", "user=bob,host=probe.invalid,addr=192.0.2.1")
		rep, err := sshd.Validate(context.Background(), h, h.request(sshd.Change{Path: sshd.HardnDropIn, Data: []byte(candidate)}))
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if e, ok := h.Entry(h.work + "/sshd_config.d/40-redhat-crypto-policies.conf"); ok {
			t.Errorf("copy left behind: %q", e.Data)
		}
		want := []string{sshd.MainConfig, sshd.HardnDropIn, redhatCrypto, sshd.CryptoPolicyFile, sshd.DropInDir + "/50-cloud-init.conf"}
		if !slices.Equal(rep.Files, want) {
			t.Errorf("Files = %q, want %q", rep.Files, want)
		}
		var copied string
		for _, c := range h.Calls() {
			if c.Op == "write-atomic" && c.Path == h.work+"/sshd_config.d/40-redhat-crypto-policies.conf" {
				copied = string(c.Data)
			}
		}
		if copied != "# crypto policy\n"+sshd.CryptoPolicyInclude+"\n" {
			t.Errorf("the copy's drop-in = %q, want it as on the host", copied)
		}
		h.checkClean(t)
	}
}

func TestValidateDropIn(t *testing.T) {
	h := newHost(t, stockMain)
	h.script(hostfake.Response{}, "port 2230\npermitrootlogin prohibit-password\n")
	spec := "user=bob,host=probe.invalid,addr=192.0.2.1"
	h.Respond(hostfake.Response{Stdout: "passwordauthentication yes\n"}, "sshd", "-T", "-f", h.cfg, "-C", spec)

	rep, err := sshd.Validate(context.Background(), h, h.request(sshd.Change{Path: sshd.HardnDropIn, Data: []byte(candidate)}))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	wantRuns := [][]string{
		{"sshd", "-t", "-f", h.cfg},
		{"sshd", "-T", "-f", h.cfg},
		{"sshd", "-T", "-f", h.cfg, "-C", spec},
	}
	if runs := h.Runs(); !slices.EqualFunc(runs, wantRuns, slices.Equal) {
		t.Errorf("runs = %q, want %q", runs, wantRuns)
	}
	wantSeen := map[string]string{
		h.cfg:                                   strings.Replace(stockMain, sshd.IncludeLine, "Include "+h.work+"/sshd_config.d/*.conf", 1),
		h.work + "/sshd_config.d/00-hardn.conf": candidate,
		h.work + "/sshd_config.d/50-cloud-init.conf": "PasswordAuthentication yes\n",
	}
	for p, want := range wantSeen {
		if h.seen[p] != want {
			t.Errorf("copy %s = %q, want %q", p, h.seen[p], want)
		}
	}
	if len(h.seen) != len(wantSeen) {
		t.Errorf("copy holds %d files, want %d (the leftover temporary file is not read by sshd)", len(h.seen), len(wantSeen))
	}
	if rep.Include.Mode != sshd.IncludeFirst || len(rep.Findings) != 0 || rep.Cleanup != nil {
		t.Errorf("Include = %v, Findings = %+v, Cleanup = %v; want IncludeFirst, none, nil", rep.Include.Mode, rep.Findings, rep.Cleanup)
	}
	if want := []string{sshd.MainConfig, sshd.HardnDropIn, sshd.DropInDir + "/50-cloud-init.conf"}; !slices.Equal(rep.Files, want) {
		t.Errorf("Files = %q, want %q", rep.Files, want)
	}
	if string(rep.Effective) != "port 2230\npermitrootlogin prohibit-password\n" {
		t.Errorf("Effective = %q", rep.Effective)
	}
	if len(rep.Probes) != 1 || rep.Probes[0].Block != (sshd.Block{File: sshd.MainConfig, Line: 8, Text: "Match User bob"}) ||
		rep.Probes[0].Conn.User != "bob" || len(rep.Probes[0].Reasons) != 0 || string(rep.Probes[0].Output) != "passwordauthentication yes\n" {
		t.Errorf("Probes = %+v", rep.Probes)
	}
	if e, _ := h.Entry(sshd.HardnDropIn); e.Data != nil {
		t.Errorf("%s was written", sshd.HardnDropIn)
	}
	h.checkClean(t)
}

func TestValidateSSHDFailures(t *testing.T) {
	d := "/sshd-validate/sshd_config.d/00-hardn.conf"
	tests := []struct {
		name  string
		r     hostfake.Response
		check func(*testing.T, error)
	}{
		{"Port in Match", hostfake.Response{ExitCode: 255, Stderr: "RUN" + d + " line 3: Directive 'Port' is not allowed within a Match block\n"},
			wantConfig(sshd.Problem{File: sshd.HardnDropIn, Line: 3, Message: "Directive 'Port' is not allowed within a Match block"})},
		{"bad option", hostfake.Response{ExitCode: 255, Stderr: "RUN" + d + ": line 2: Bad configuration option: Foo\nRUN" + d + ": terminating, 1 bad configuration options\n"},
			wantConfig(sshd.Problem{File: sshd.HardnDropIn, Line: 2, Message: "Bad configuration option: Foo"})},
		{"bad value in sshd_config", hostfake.Response{ExitCode: 255, Stderr: "RUN/sshd-validate/sshd_config line 5: unsupported option \"maybe\".\n"},
			wantConfig(sshd.Problem{File: sshd.MainConfig, Line: 5, Message: `unsupported option "maybe".`})},
		{"no privsep dir", hostfake.Response{ExitCode: 255, Stderr: "Missing privilege separation directory: /run/sshd\n"}, wantEnv(sshd.EnvPrivsepDir, "/run/sshd")},
		{"privsep dir not root's", hostfake.Response{ExitCode: 255, Stderr: "/run/sshd must be owned by root and not group or world-writable.\n"}, wantEnv(sshd.EnvPrivsepDir, "/run/sshd")},
		{"privsep message ends with CRLF", hostfake.Response{ExitCode: 255, Stderr: "Missing privilege separation directory: /run/sshd\r\n"}, wantEnv(sshd.EnvPrivsepDir, "/run/sshd")},
		{"no privsep user", hostfake.Response{ExitCode: 255, Stderr: "Privilege separation user sshd does not exist\r\n"},
			wantEnvErr(sshd.EnvError{Kind: sshd.EnvPrivsepUser, User: "sshd"})},
		{"privsep text quoted in a config message", hostfake.Response{ExitCode: 255,
			Stderr: "RUN" + d + " line 2: unsupported option \"/x must be owned by root and not group or world-writable.\".\n"},
			wantConfig(sshd.Problem{File: sshd.HardnDropIn, Line: 2, Message: `unsupported option "/x must be owned by root and not group or world-writable.".`})},
		{"no hostkeys", hostfake.Response{ExitCode: 1, Stderr: "sshd: no hostkeys available -- exiting.\n"}, wantEnv(sshd.EnvHostKeys, "")},
		{"other exit 1", hostfake.Response{ExitCode: 1, Stderr: "RUN/sshd-validate/sshd_config: Invalid test mode specification x\n"},
			wantUnexpected(1, sshd.MainConfig+": Invalid test mode specification x")},
		{"killed", hostfake.Response{ExitCode: -1}, wantUnexpected(-1, "")},
		{"failed to start", hostfake.Response{Err: fmt.Errorf("host: run sshd -t -f %s/sshd-validate/sshd_config: %w", runDir, errBroken)},
			func(t *testing.T, err error) {
				t.Helper()
				wantIs(errBroken)(t, err)
				wantNot(sshd.ErrConfig, sshd.ErrEnvironment, sshd.ErrUnexpected)(t, err)
			}},
		{"not installed", hostfake.Response{Err: host.ErrNotFound}, wantIs(host.ErrNotFound)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t, stockMain)
			tt.r.Stderr = strings.ReplaceAll(tt.r.Stderr, "RUN", h.run)
			h.script(tt.r, "")
			_, err := sshd.Validate(context.Background(), h, h.request(sshd.Change{Path: sshd.HardnDropIn, Data: []byte(candidate)}))
			tt.check(t, err)
			if n := len(h.Runs()); n != 1 {
				t.Errorf("%d sshd runs, want 1: nothing runs after a failed sshd -t", n)
			}
			if err != nil && strings.Contains(err.Error(), h.run) {
				t.Errorf("error %q names the copy, not the real file", err)
			}
			h.checkClean(t)
		})
	}
}

func wantConfig(p sshd.Problem) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()
		var ce *sshd.ConfigError
		var xe *host.ExitError
		if !errors.As(err, &ce) || !errors.Is(err, sshd.ErrConfig) || errors.Is(err, sshd.ErrEnvironment) || !errors.As(err, &xe) {
			t.Fatalf("err = %v, want a *ConfigError wrapping a *host.ExitError", err)
		}
		if len(ce.Problems) == 0 || ce.Problems[0] != p {
			t.Errorf("Problems = %+v, want %+v first", ce.Problems, p)
		}
	}
}

var errBroken = errors.New("broken pipe")

func wantEnv(kind sshd.EnvKind, path string) func(*testing.T, error) {
	return wantEnvErr(sshd.EnvError{Kind: kind, Path: path})
}

// wantEnvErr wants an *EnvError from a run with want's Kind, Path, User and
// HostKeysSet.
func wantEnvErr(want sshd.EnvError) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()
		var ee *sshd.EnvError
		if !errors.As(err, &ee) || !errors.Is(err, sshd.ErrEnvironment) || errors.Is(err, sshd.ErrConfig) {
			t.Fatalf("err = %v, want an *EnvError", err)
		}
		if ee.Kind != want.Kind || ee.Path != want.Path || ee.User != want.User || ee.HostKeysSet != want.HostKeysSet || ee.Argv == nil {
			t.Errorf("EnvError = %+v, want %+v from a run", ee, want)
		}
	}
}

// wantUnexpected wants an *UnexpectedError with code, whose last Tail line
// is last ("" for no output).
func wantUnexpected(code int, last string) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()
		var ue *sshd.UnexpectedError
		var xe *host.ExitError
		if !errors.As(err, &ue) || !errors.Is(err, sshd.ErrUnexpected) || !errors.As(err, &xe) || errors.Is(err, sshd.ErrConfig) {
			t.Fatalf("err = %v, want an *UnexpectedError wrapping a *host.ExitError", err)
		}
		got := ""
		if len(ue.Tail) > 0 {
			got = ue.Tail[len(ue.Tail)-1]
		}
		if ue.ExitCode != code || got != last {
			t.Errorf("UnexpectedError = %+v, want exit %d, last line %q", ue, code, last)
		}
		if last != "" && !strings.Contains(err.Error(), last) {
			t.Errorf("err = %q, want it to show %q", err, last)
		}
	}
}

func wantIs(target error) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, target) {
			t.Fatalf("err = %v, want %v", err, target)
		}
	}
}

func wantNot(targets ...error) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()
		for _, target := range targets {
			if err == nil || errors.Is(err, target) {
				t.Fatalf("err = %v, want an error that is not %v", err, target)
			}
		}
	}
}

// TestValidateRefuses covers requests and hosts Validate refuses before it
// runs sshd or writes anything.
func TestValidateRefuses(t *testing.T) {
	drop := sshd.Change{Path: sshd.HardnDropIn, Data: []byte(candidate)}
	tests := []struct {
		name  string
		setup func(*fakeHost, *sshd.Request)
		want  error
	}{
		{"relative run dir", func(_ *fakeHost, r *sshd.Request) { r.RunDir = "run" }, fs.ErrInvalid},
		{"root as run dir", func(_ *fakeHost, r *sshd.Request) { r.RunDir = "/" }, fs.ErrInvalid},
		{"space in run dir", func(h *fakeHost, r *sshd.Request) { r.RunDir = h.run + "/a b"; h.AddDir(r.RunDir, 0o700, 0, 0) }, fs.ErrInvalid},
		{"missing run dir", func(h *fakeHost, r *sshd.Request) { r.RunDir = h.run + "/gone" }, fs.ErrNotExist},
		{"group-writable run dir", func(h *fakeHost, _ *sshd.Request) { h.AddDir(h.run, 0o770, 0, 0) }, sshd.ErrUnsafeRunDir},
		{"run dir not root's", func(h *fakeHost, _ *sshd.Request) { h.AddDir(h.run, 0o700, 1000, 1000) }, sshd.ErrUnsafeRunDir},
		{"world-writable parent", func(h *fakeHost, r *sshd.Request) {
			h.AddDir(h.run+"/tmp", 0o777, 0, 0)
			r.RunDir = h.run + "/tmp/r"
			h.AddDir(r.RunDir, 0o700, 0, 0)
		}, sshd.ErrUnsafeRunDir},
		{"run dir through a symlink", func(h *fakeHost, r *sshd.Request) { h.AddSymlink(h.run+"/link", h.run); r.RunDir = h.run + "/link" }, sshd.ErrUnsafeRunDir},
		{"work dir exists", func(h *fakeHost, _ *sshd.Request) { h.AddDir(h.work, 0o700, 0, 0) }, fs.ErrExist},
		{"path outside", func(_ *fakeHost, r *sshd.Request) {
			r.Changes = []sshd.Change{{Path: "/etc/ssh/ssh_config", Data: []byte("x")}}
		}, fs.ErrInvalid},
		{"not read by the Include", func(_ *fakeHost, r *sshd.Request) {
			r.Changes = []sshd.Change{{Path: sshd.DropInDir + "/00-hardn.cfg"}}
		}, fs.ErrInvalid},
		{"hidden drop-in", func(_ *fakeHost, r *sshd.Request) {
			r.Changes = []sshd.Change{{Path: sshd.DropInDir + "/.00-hardn.conf"}}
		}, fs.ErrInvalid},
		{"remove sshd_config", func(_ *fakeHost, r *sshd.Request) { r.Changes = []sshd.Change{{Path: sshd.MainConfig, Remove: true}} }, fs.ErrInvalid},
		{"changed twice", func(_ *fakeHost, r *sshd.Request) { r.Changes = append(r.Changes, drop) }, fs.ErrInvalid},
		{"bad probe", func(_ *fakeHost, r *sshd.Request) {
			r.Conn = sshd.Conn{User: "alice,bob", Host: "h", Addr: "192.0.2.1"}
		}, fs.ErrInvalid},
		{"drop-in without Include", func(h *fakeHost, _ *sshd.Request) { h.AddFile(sshd.MainConfig, []byte("Port 22\n"), 0o644, 0, 0) }, sshd.ErrNotIncluded},
		{"second Include", func(h *fakeHost, _ *sshd.Request) {
			h.AddFile(sshd.MainConfig, []byte(stockMain+"Include /etc/ssh/extra/*.conf\n"), 0o644, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"Include in a drop-in", func(h *fakeHost, _ *sshd.Request) {
			h.AddFile(sshd.DropInDir+"/10-x.conf", []byte("Include /etc/ssh/sshd_config.d/*.conf\n"), 0o644, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		// The crypto policy's Include (Rocky Linux, AlmaLinux) is followed
		// only to a regular file of crypto settings.
		{"the crypto policy's Include, the policy missing", func(h *fakeHost, _ *sshd.Request) {
			h.AddFile(redhatCrypto, []byte(sshd.CryptoPolicyInclude+"\n"), 0o600, 0, 0)
		}, fs.ErrNotExist},
		{"the crypto policy setting a login option", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText+"PasswordAuthentication yes\n")
		}, sshd.ErrUnsupportedInclude},
		{"the crypto policy with a Match", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText+"Match User bob\n  Ciphers aes256-ctr\n")
		}, sshd.ErrUnsupportedInclude},
		{"the crypto policy including another file", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, "Include /etc/ssh/extra.conf\n")
		}, sshd.ErrUnsupportedInclude},
		{"the crypto policy a directory", func(h *fakeHost, _ *sshd.Request) {
			h.AddFile(redhatCrypto, []byte(sshd.CryptoPolicyInclude+"\n"), 0o600, 0, 0)
			h.AddDir(sshd.CryptoPolicyFile, 0o755, 0, 0)
		}, sshd.ErrNotRegular},
		{"the crypto policy with a NUL", func(h *fakeHost, _ *sshd.Request) { cryptoPolicy(h, "Ciphers aes256-ctr\x00\n") }, fs.ErrInvalid},
		{"the crypto policy's Include beside another", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText)
			h.AddFile(redhatCrypto, []byte(sshd.CryptoPolicyInclude+"\nInclude /etc/ssh/extra.conf\n"), 0o600, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"another path of the policy", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText)
			h.AddFile(redhatCrypto, []byte("Include /usr/share/crypto-policies/DEFAULT/opensshserver.txt\n"), 0o600, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		// Only the exact line: a variant sshd would read the same is refused
		// too (over-refusal is safe); a second argument would read the real
		// sshd_config.d into the copy.
		{"the policy's Include with a second argument", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText)
			h.AddFile(redhatCrypto, []byte(sshd.CryptoPolicyInclude+" /etc/ssh/sshd_config.d/*.conf\n"), 0o600, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"the policy's Include in lower case", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText)
			h.AddFile(redhatCrypto, []byte("include "+sshd.CryptoPolicyFile+"\n"), 0o600, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"the policy's Include with a doubled space", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText)
			h.AddFile(redhatCrypto, []byte("Include  "+sshd.CryptoPolicyFile+"\n"), 0o600, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"the policy's path in quotes", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText)
			h.AddFile(redhatCrypto, []byte("Include \""+sshd.CryptoPolicyFile+"\"\n"), 0o600, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"the policy's Include with a trailing comment", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText)
			h.AddFile(redhatCrypto, []byte(sshd.CryptoPolicyInclude+" # policy\n"), 0o600, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"the policy's Include after a leading =", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText)
			h.AddFile(redhatCrypto, []byte("Include="+sshd.CryptoPolicyFile+"\n"), 0o600, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"the policy's Include under a Match", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, "PubkeyAcceptedAlgorithms ssh-ed25519\n")
			h.AddFile(redhatCrypto, []byte("Match User root\n"+sshd.CryptoPolicyInclude+"\n"), 0o600, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"the crypto policy's Include in sshd_config", func(h *fakeHost, _ *sshd.Request) {
			cryptoPolicy(h, policyText)
			h.AddFile(sshd.MainConfig, []byte(stockMain+sshd.CryptoPolicyInclude+"\n"), 0o644, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		{"directory drop-in", func(h *fakeHost, _ *sshd.Request) { h.AddDir(sshd.DropInDir+"/10-x.conf", 0o755, 0, 0) }, sshd.ErrNotRegular},
		{"no sshd_config", func(h *fakeHost, _ *sshd.Request) { _ = h.Remove(context.Background(), sshd.MainConfig) }, fs.ErrNotExist},
		{"sshd_config a directory", func(h *fakeHost, _ *sshd.Request) { h.AddDir(sshd.MainConfig, 0o755, 0, 0) }, sshd.ErrNotRegular},
		{"removed and written", func(_ *fakeHost, r *sshd.Request) {
			r.Changes = []sshd.Change{{Path: sshd.DropInDir + "/50-cloud-init.conf", Remove: true, Data: []byte("ignored")}}
		}, fs.ErrInvalid},
		// sshd's strdelim reads "=Include" as Include, of the real directory.
		{"Include after a leading =", func(h *fakeHost, _ *sshd.Request) {
			h.AddFile(sshd.MainConfig, []byte(stockMain+"=Include /etc/ssh/sshd_config.d/*.conf\n"), 0o644, 0, 0)
		}, sshd.ErrUnsupportedInclude},
		// sshd's loader joins "Inc" and "lude" at the NUL into an Include.
		{"NUL in sshd_config", func(h *fakeHost, _ *sshd.Request) {
			h.AddFile(sshd.MainConfig, []byte(stockMain+"Inc\x00\nlude /etc/ssh/sshd_config.d/*.conf\n"), 0o644, 0, 0)
		}, fs.ErrInvalid},
		{"NUL in a drop-in", func(h *fakeHost, _ *sshd.Request) {
			h.AddFile(sshd.DropInDir+"/10-x.conf", []byte("Port 22\x00\n"), 0o644, 0, 0)
		}, fs.ErrInvalid},
		{"NUL in the candidate", func(_ *fakeHost, r *sshd.Request) {
			r.Changes = []sshd.Change{{Path: sshd.HardnDropIn, Data: []byte("Port 2230\x00\n")}}
		}, fs.ErrInvalid},
		{"Proxmox VE without /run/sshd", func(h *fakeHost, r *sshd.Request) {
			_ = h.RemoveAll(context.Background(), sshd.PrivsepDir)
			r.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9"}
		}, sshd.ErrEnvironment},
		{"no /run/sshd", func(h *fakeHost, _ *sshd.Request) { _ = h.RemoveAll(context.Background(), sshd.PrivsepDir) }, sshd.ErrEnvironment},
		{"/run/sshd group-writable", func(h *fakeHost, r *sshd.Request) {
			h.AddDir(sshd.PrivsepDir, 0o775, 0, 0)
			r.Platform = detect.Platform{Distro: detect.DistroOther, Like: detect.DistroUbuntu}
		}, sshd.ErrEnvironment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t, stockMain)
			req := h.request(drop)
			tt.setup(h, &req)
			before := len(h.Calls())
			_, err := sshd.Validate(context.Background(), h, req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			for _, c := range h.Calls()[before:] {
				if c.Op != "lstat" && c.Op != "stat" && c.Op != "read" && c.Op != "readdir" {
					t.Errorf("%s: only reads before a refusal", c)
				}
			}
		})
	}
	t.Run("pre-check", func(t *testing.T) {
		h := newHost(t, stockMain)
		_ = h.RemoveAll(context.Background(), sshd.PrivsepDir)
		_, err := sshd.Validate(context.Background(), h, h.request(drop))
		var ee *sshd.EnvError
		if !errors.As(err, &ee) || ee.Kind != sshd.EnvPrivsepDir || ee.Path != sshd.PrivsepDir || ee.Argv != nil || !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("err = %#v, want the pre-check's EnvError for %s, with no Argv", err, sshd.PrivsepDir)
		}
		if want := "privilege separation directory " + sshd.PrivsepDir + ": "; !strings.Contains(err.Error(), want) || len(h.Runs()) != 0 {
			t.Errorf("err = %q after %d runs, want it to contain %q, and no run", err, len(h.Runs()), want)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		h := newHost(t, stockMain)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := sshd.Validate(ctx, h, h.request(drop)); !errors.Is(err, context.Canceled) || len(h.Calls()) != 0 {
			t.Fatalf("err = %v with %d calls, want context.Canceled and none", err, len(h.Calls()))
		}
	})
}

// TestValidateMainFile is no-Include mode, on Alpine: the candidate is the
// whole sshd_config with hardn's block on top; the drop-ins are not read and
// /run/sshd is not required.
func TestValidateMainFile(t *testing.T) {
	main := "# BEGIN hardn\nPort 2230\n# END hardn\nSubsystem sftp internal-sftp\n"
	h := newHost(t, "Port 22\n")
	_ = h.RemoveAll(context.Background(), sshd.PrivsepDir)
	h.script(hostfake.Response{Stderr: "RUN/sshd-validate/sshd_config line 4: Deprecated option UsePrivilegeSeparation\n"}, "port 2230\n")
	h.Handler = wrapStderr(h.Handler, h.run)
	req := h.request(sshd.Change{Path: sshd.MainConfig, Data: []byte(main)},
		sshd.Change{Path: sshd.DropInDir + "/50-cloud-init.conf", Remove: true}) // allowed, and changes nothing sshd reads
	req.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24"}

	rep, err := sshd.Validate(context.Background(), h, req)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if h.seen[h.cfg] != main || len(h.seen) != 1 {
		t.Errorf("copy = %q, want sshd_config alone, unchanged", h.seen)
	}
	want := []sshd.FindingKind{sshd.FindingClobberable, sshd.FindingWarning}
	var got []sshd.FindingKind
	for _, f := range rep.Findings {
		got = append(got, f.Kind)
	}
	if !slices.Equal(got, want) || rep.Findings[1].File != sshd.MainConfig || rep.Findings[1].Line != 4 {
		t.Errorf("Findings = %+v, want %v with the warning on %s line 4", rep.Findings, want, sshd.MainConfig)
	}
	if rep.Include.Mode != sshd.IncludeNone || !slices.Equal(rep.Files, []string{sshd.MainConfig}) {
		t.Errorf("Include = %v, Files = %q", rep.Include.Mode, rep.Files)
	}
	h.checkClean(t)
}

// wrapStderr replaces RUN in the -t response's stderr with the run directory.
func wrapStderr(next func([]string, []byte) hostfake.Response, run string) func([]string, []byte) hostfake.Response {
	return func(argv []string, stdin []byte) hostfake.Response {
		r := next(argv, stdin)
		r.Stderr = strings.ReplaceAll(r.Stderr, "RUN", run)
		return r
	}
}

// TestValidateProbes: a Match in a drop-in is probed on its own; one the
// spec cannot target is still probed and reported; a removed drop-in is not
// copied; a failed probe fails the validation.
func TestValidateProbes(t *testing.T) {
	h := newHost(t, sshd.IncludeLine+"\nMatch Group admins\n  PasswordAuthentication yes\n")
	h.AddFile(sshd.DropInDir+"/40-lan.conf", []byte("Match Address 192.0.2.128/25\n  PermitRootLogin yes\n"), 0o644, 0, 0)
	h.script(hostfake.Response{}, "")
	base := sshd.DefaultConn().Spec()
	h.Respond(hostfake.Response{Stdout: "passwordauthentication no\n"}, "sshd", "-T", "-f", h.cfg, "-C", base)
	h.Respond(hostfake.Response{ExitCode: 255, Stderr: h.run + "/sshd-validate/sshd_config.d/40-lan.conf line 2: Bad Match condition\n"},
		"sshd", "-T", "-f", h.cfg, "-C", "user=hardn-probe,host=probe.invalid,addr=192.0.2.128")

	rep, err := sshd.Validate(context.Background(), h, h.request(sshd.Change{Path: sshd.DropInDir + "/50-cloud-init.conf", Remove: true}))
	wantConfig(sshd.Problem{File: sshd.DropInDir + "/40-lan.conf", Line: 2, Message: "Bad Match condition"})(t, err)
	if _, ok := h.seen[h.work+"/sshd_config.d/50-cloud-init.conf"]; ok {
		t.Error("the removed drop-in was copied")
	}
	if len(rep.Probes) != 1 || rep.Probes[0].Conn != sshd.DefaultConn() || len(rep.Probes[0].Reasons) != 1 {
		t.Errorf("Probes = %+v, want the Group block probed with the default connection", rep.Probes)
	}
	if len(rep.Findings) != 1 || rep.Findings[0].Kind != sshd.FindingMatchUntargeted || rep.Findings[0].Line != 2 {
		t.Errorf("Findings = %+v, want the Group block untargeted", rep.Findings)
	}
	h.checkClean(t)
}

// failRemoveAll is a host whose RemoveAll fails.
type failRemoveAll struct{ *fakeHost }

var errRemove = errors.New("injected")

func (failRemoveAll) RemoveAll(context.Context, string) error { return errRemove }

func TestValidateCleanupError(t *testing.T) {
	h := newHost(t, stockMain)
	h.script(hostfake.Response{}, "")
	h.Respond(hostfake.Response{}, "sshd", "-T", "-f", h.cfg, "-C", "user=bob,host=probe.invalid,addr=192.0.2.1")
	rep, err := sshd.Validate(context.Background(), failRemoveAll{h}, h.request())
	if err != nil || !errors.Is(rep.Cleanup, errRemove) {
		t.Fatalf("err = %v, Cleanup = %v; want nil and the injected error", err, rep.Cleanup)
	}
}

// TestValidateIncludeNotFirst: an Include that is not first is reported and
// still rewritten; a Match value the spec cannot carry falls back to the
// default connection.
func TestValidateIncludeNotFirst(t *testing.T) {
	h := newHost(t, "Port 22\n"+sshd.IncludeLine+"\nMatch User \"a b\"\n  PasswordAuthentication yes\n")
	h.script(hostfake.Response{}, "port 22\n")
	h.Respond(hostfake.Response{}, "sshd", "-T", "-f", h.cfg, "-C", sshd.DefaultConn().Spec())
	rep, err := sshd.Validate(context.Background(), h, h.request())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if want := "Port 22\nInclude " + h.work + "/sshd_config.d/*.conf\n"; !strings.HasPrefix(h.seen[h.cfg], want) {
		t.Errorf("copy = %q, want it to start %q", h.seen[h.cfg], want)
	}
	if len(rep.Findings) != 2 || rep.Findings[0].Kind != sshd.FindingIncludeNotFirst || rep.Findings[0].Line != 2 ||
		rep.Findings[1].Kind != sshd.FindingMatchUntargeted {
		t.Errorf("Findings = %+v, want include-not-first on line 2, then match-untargeted", rep.Findings)
	}
	h.checkClean(t)
}

func TestValidateEffectiveFails(t *testing.T) {
	h := newHost(t, stockMain)
	h.Respond(hostfake.Response{}, "sshd", "-t", "-f", h.cfg)
	h.Respond(hostfake.Response{ExitCode: 255, Stderr: "Missing privilege separation directory: /run/sshd\n"}, "sshd", "-T", "-f", h.cfg)
	if _, err := sshd.Validate(context.Background(), h, h.request()); !errors.Is(err, sshd.ErrEnvironment) || len(h.Runs()) != 2 {
		t.Fatalf("err = %v after %d runs, want ErrEnvironment after sshd -T, with no probe", err, len(h.Runs()))
	}
	h.checkClean(t)
}

// TestValidateProbeReasons: a block sshd never applies (negations alone; a
// Host name under UseDNS no) is probed and reported, not shown as applied;
// one whose first address is negated is probed with another.
func TestValidateProbeReasons(t *testing.T) {
	main := sshd.IncludeLine + "\nMatch User !root\n  PasswordAuthentication yes\n" +
		"Match Host *.example.net\n  PermitRootLogin no\n" +
		"Match Address 192.0.2.128/25,!192.0.2.128/26\n  PubkeyAuthentication no\n"
	h := newHost(t, main)
	h.script(hostfake.Response{}, "usedns no\n")
	h.Respond(hostfake.Response{}, "sshd", "-T", "-f", h.cfg, "-C", sshd.DefaultConn().Spec())
	h.Respond(hostfake.Response{Stdout: "pubkeyauthentication no\n"}, "sshd", "-T", "-f", h.cfg, "-C", "user=hardn-probe,host=probe.invalid,addr=192.0.2.255")

	rep, err := sshd.Validate(context.Background(), h, h.request())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := []struct {
		addr   string
		reason string
	}{
		{"192.0.2.1", "User !root: only negated patterns"},
		{"192.0.2.1", "UseDNS is no"},
		{"192.0.2.255", ""},
	}
	if len(rep.Probes) != len(want) {
		t.Fatalf("Probes = %+v, want %d", rep.Probes, len(want))
	}
	for i, w := range want {
		p := rep.Probes[i]
		joined := strings.Join(p.Reasons, "; ")
		if p.Conn.Addr != w.addr || w.reason == "" && joined != "" || !strings.Contains(joined, w.reason) {
			t.Errorf("probe %d (%s) = %+v, want addr %s and a reason containing %q", i, p.Block.Text, p, w.addr, w.reason)
		}
	}
	if len(rep.Findings) != 2 || rep.Findings[0].Line != 2 || rep.Findings[1].Line != 4 || rep.Findings[1].Kind != sshd.FindingMatchUntargeted {
		t.Errorf("Findings = %+v, want match-untargeted on lines 2 and 4", rep.Findings)
	}
	h.checkClean(t)
}

// TestValidateHostKeysSet: with HostKey lines, "no hostkeys" names the
// configuration's own key files.
func TestValidateHostKeysSet(t *testing.T) {
	h := newHost(t, stockMain)
	h.script(hostfake.Response{ExitCode: 1, Stderr: "Unable to load host key: /etc/ssh/ssh_host_x_key\nsshd: no hostkeys available -- exiting.\n"}, "")
	_, err := sshd.Validate(context.Background(), h, h.request(sshd.Change{Path: sshd.HardnDropIn, Data: []byte("HostKey /etc/ssh/ssh_host_x_key\n")}))
	wantEnvErr(sshd.EnvError{Kind: sshd.EnvHostKeys, HostKeysSet: true})(t, err)
	if !strings.Contains(err.Error(), "HostKey files") {
		t.Errorf("err = %q, want it to name the configuration's HostKey files", err)
	}
	h.checkClean(t)
}

// TestValidateNoDropInDir: a missing sshd_config.d reads as empty in
// IncludeFirst mode, so the copy holds sshd_config and the candidate.
func TestValidateNoDropInDir(t *testing.T) {
	h := newHost(t, stockMain)
	_ = h.RemoveAll(context.Background(), sshd.DropInDir)
	h.script(hostfake.Response{}, "")
	h.Respond(hostfake.Response{}, "sshd", "-T", "-f", h.cfg, "-C", "user=bob,host=probe.invalid,addr=192.0.2.1")
	rep, err := sshd.Validate(context.Background(), h, h.request(sshd.Change{Path: sshd.HardnDropIn, Data: []byte(candidate)}))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if want := []string{sshd.MainConfig, sshd.HardnDropIn}; !slices.Equal(rep.Files, want) || len(h.seen) != 2 {
		t.Errorf("Files = %q, copy = %q; want %q", rep.Files, h.seen, want)
	}
	h.checkClean(t)
}

// failing is a host whose op on path fails with errInjected.
type failing struct {
	*fakeHost
	op, path string
}

var errInjected = errors.New("injected")

func (f failing) fail(op, name string) error {
	if op == f.op && name == f.path {
		return errInjected
	}
	return nil
}

func (f failing) MkdirAll(ctx context.Context, name string, perm fs.FileMode) error {
	if err := f.fail("mkdir", name); err != nil {
		return err
	}
	return f.fakeHost.MkdirAll(ctx, name, perm)
}

func (f failing) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if err := f.fail("write", name); err != nil {
		return err
	}
	return f.fakeHost.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

func (f failing) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if err := f.fail("readdir", name); err != nil {
		return nil, err
	}
	return f.fakeHost.ReadDir(ctx, name)
}

func (f failing) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := f.fail("read", name); err != nil {
		return nil, err
	}
	return f.fakeHost.ReadFile(ctx, name)
}

// TestValidateHostFailures: a failed host operation stops the validation
// before sshd runs, and the copy is removed.
func TestValidateHostFailures(t *testing.T) {
	work := runDir + "/" + sshd.WorkDir
	tests := []struct{ op, path string }{
		{"read", sshd.MainConfig},
		{"readdir", sshd.DropInDir},
		{"read", sshd.DropInDir + "/50-cloud-init.conf"},
		{"mkdir", work + "/sshd_config.d"},
		{"write", work + "/sshd_config"},
		{"write", work + "/sshd_config.d/50-cloud-init.conf"},
	}
	for _, tt := range tests {
		t.Run(tt.op+" "+tt.path, func(t *testing.T) {
			h := newHost(t, stockMain)
			rep, err := sshd.Validate(context.Background(), failing{h, tt.op, tt.path}, h.request(sshd.Change{Path: sshd.HardnDropIn, Data: []byte(candidate)}))
			if !errors.Is(err, errInjected) || len(h.Runs()) != 0 || rep.Cleanup != nil {
				t.Fatalf("err = %v after %d runs, Cleanup = %v; want the injected error, no run, a clean removal", err, len(h.Runs()), rep.Cleanup)
			}
			h.checkClean(t)
		})
	}
}
