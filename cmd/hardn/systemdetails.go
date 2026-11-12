package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// SystemDetailsForm is what hardn system-details' flags ask for: the JSON document or
// the report, and the warnings to say first.
type SystemDetailsForm struct {
	JSON     bool
	Warnings []string
}

// The flags' words (refactor plan slice 1, unit (d)), adopted 2026-10-31.
const (
	sdUseJSON     = "use --output json"
	sdDeprecated  = "%s is deprecated: %s"
	sdRefused     = "hardn: %s is refused: %s"
	sdTextDefault = "text is the default"
	sdNoFilter    = "nothing filters the report"
	sdUseRedirect = "use > file"
	sdAlone       = "run it without system-details"
)

// rootOperations are root's flags that run something, each with the name a refusal
// gives it. Root's others (-f, -u, -n, -v, --no-color) change nothing here.
var rootOperations = []struct{ flag, name string }{
	{"print-logs", "-p"},
	{"create-user", "-c"},
	{"disable-root", "-d"},
	{"configure-dns", "-g"},
	{"configure-ufw", "-w"},
	{"run-all", "-r"},
}

// systemDetailsFlags are the command's flags as given. -E, -S, -O, --json and --yaml
// never did anything and are in no release, so only --output shows in its help.
type systemDetailsFlags struct {
	output, format, section, export string
	json, yaml                      bool
}

// form is decision 1 (a) (gallery-system-details.md §5): --output json is the JSON
// form; --json and -O json become it with a warning; -O text is accepted with a
// warning; --yaml, -O yaml, -S and -E are refused, as is any other value of --output
// or -O. A flag counts as given whatever its value, as --yaml=false. Root's operation
// flags, which this command would not run, are refused too. given says which flags
// were on the command line. A refusal is the error, for exit status 2 (output.ExitUsage).
func (f systemDetailsFlags) form(given func(name string) bool) (SystemDetailsForm, error) {
	for _, op := range rootOperations {
		if given(op.flag) {
			return SystemDetailsForm{}, fmt.Errorf(sdRefused, op.name, sdAlone)
		}
	}
	switch {
	case given("yaml"):
		return SystemDetailsForm{}, fmt.Errorf(sdRefused, "--yaml", sdUseJSON)
	case given("format") && f.format != "json" && f.format != "text":
		return SystemDetailsForm{}, fmt.Errorf(sdRefused, "-O "+f.format, sdUseJSON)
	case given("output") && f.output != "json":
		return SystemDetailsForm{}, fmt.Errorf(sdRefused, "--output "+f.output, sdUseJSON)
	case given("section"):
		return SystemDetailsForm{}, fmt.Errorf(sdRefused, "-S", sdNoFilter)
	case given("export"):
		return SystemDetailsForm{}, fmt.Errorf(sdRefused, "-E", sdUseRedirect)
	}
	form := SystemDetailsForm{JSON: given("output")}
	if given("json") {
		form.JSON = form.JSON || f.json
		form.Warnings = append(form.Warnings, fmt.Sprintf(sdDeprecated, "--json", sdUseJSON))
	}
	if given("format") {
		if f.format == "json" {
			form.JSON = true
			form.Warnings = append(form.Warnings, fmt.Sprintf(sdDeprecated, "-O json", sdUseJSON))
		} else {
			form.Warnings = append(form.Warnings, fmt.Sprintf(sdDeprecated, "-O text", sdTextDefault))
		}
	}
	return form, nil
}

// systemDetailsCmd returns the system-details command, whose flags it reads and whose
// work is run's, cmd/hardn's, which has the host and the terminal: run gets the form,
// or the refusal.
func systemDetailsCmd(run func(SystemDetailsForm, error)) *cobra.Command {
	var f systemDetailsFlags
	cmd := &cobra.Command{
		Use:   "system-details",
		Short: "Display detailed system summary",
		Long:  `Print the host's report: OS and kernel, host and IPs, users, CPU and load, disk, memory, last login. Save it with > file. --output json prints it as hardn's JSON document.`,
		Run: func(cmd *cobra.Command, _ []string) {
			run(f.form(cmd.Flags().Changed))
		},
	}
	cmd.Flags().StringVar(&f.output, "output", "", "Print the report in this format (json)")
	cmd.Flags().StringVarP(&f.export, "export", "E", "", "Refused: use > file")
	cmd.Flags().StringVarP(&f.section, "section", "S", "", "Refused: nothing filters the report")
	cmd.Flags().StringVarP(&f.format, "format", "O", "text", "Deprecated: use --output json")
	cmd.Flags().BoolVar(&f.json, "json", false, "Deprecated: use --output json")
	cmd.Flags().BoolVar(&f.yaml, "yaml", false, "Refused: use --output json")
	for _, name := range []string{"export", "section", "format", "json", "yaml"} {
		_ = cmd.Flags().MarkHidden(name)
	}
	return cmd
}
