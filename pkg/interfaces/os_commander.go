// pkg/interfaces/os_commander.go
package interfaces

import (
	"bytes"
	"os/exec"
)

// OSCommander is an implementation of Commander using os/exec. With
// legacyThroughHost it runs through the shared host.Real (SystemCommander).
type OSCommander struct{}

func (c OSCommander) Execute(command string, args ...string) ([]byte, error) {
	if legacyThroughHost {
		return SystemCommander{Sys: realSystem()}.Execute(command, args...)
	}
	cmd := exec.Command(command, args...)
	return cmd.CombinedOutput()
}

func (c OSCommander) ExecuteWithInput(input string, command string, args ...string) ([]byte, error) {
	if legacyThroughHost {
		return SystemCommander{Sys: realSystem()}.ExecuteWithInput(input, command, args...)
	}
	cmd := exec.Command(command, args...)

	stdin := bytes.NewBufferString(input)
	cmd.Stdin = stdin

	return cmd.CombinedOutput()
}
