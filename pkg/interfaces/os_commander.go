// pkg/interfaces/os_commander.go
package interfaces

import (
	"bytes"
	"os/exec"
)

// OSCommander is an implementation of Commander using os/exec
type OSCommander struct{}

func (c OSCommander) Execute(command string, args ...string) ([]byte, error) {
	cmd := exec.Command(command, args...)
	return cmd.CombinedOutput()
}

func (c OSCommander) ExecuteWithInput(input string, command string, args ...string) ([]byte, error) {
	cmd := exec.Command(command, args...)

	stdin := bytes.NewBufferString(input)
	cmd.Stdin = stdin

	return cmd.CombinedOutput()
}
