package steps

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The planners carry the primitives' findings on their changes
// (engine.Change.Findings), which p3-output renders. Each code is the
// primitive's own stable kind code (pinned by its TestFindingCodes), upper
// case with underscores, under the primitive's prefix: PVE_ROOT_KEY_ONLY,
// SSHD_OVERRIDDEN, UFW_SSH_NOT_ALLOWED_AT_START.

// CodeMigrateDropped is a legacy line the migration leaves out of
// 00-hardn.conf, with why (migrate.Plan.Dropped).
const CodeMigrateDropped = "SSH_MIGRATE_LINE_DROPPED"

// findingCode is kind, a primitive's code ("root-key-only"), as an
// engine.Finding's: "<PREFIX>_ROOT_KEY_ONLY", the prefix said once.
func findingCode(prefix, kind string) string {
	k := strings.ToUpper(strings.ReplaceAll(kind, "-", "_"))
	if strings.HasPrefix(k, prefix+"_") {
		return k
	}
	return prefix + "_" + k
}

// at is file:line, file alone, or "".
func at(file string, line int) string {
	if file != "" && line > 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}
	return file
}

func pveFindings(fs []pve.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		out = append(out, engine.Finding{Code: findingCode("PVE", f.Kind.String()), Detail: f.Detail, Source: at(f.File, f.Line)})
	}
	return out
}

func sshdFindings(fs []sshd.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		out = append(out, engine.Finding{Code: findingCode("SSHD", f.Kind.String()), Detail: f.Detail, Source: at(f.File, f.Line)})
	}
	return out
}

func ufwFindings(fs []ufw.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		out = append(out, engine.Finding{Code: findingCode("UFW", f.Kind.String()), Detail: strings.TrimPrefix(f.String(), f.Kind.String()+": "), Source: "ufw"})
	}
	return out
}

// droppedFindings are the legacy lines a migration leaves out, with why.
func droppedFindings(ds []migrate.Carried) []engine.Finding {
	var out []engine.Finding
	for _, d := range ds {
		out = append(out, engine.Finding{Code: CodeMigrateDropped, Detail: fmt.Sprintf("%q: %s", d.Text, d.Why), Source: at(d.File, d.Line)})
	}
	return out
}
