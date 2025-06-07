package security

import (
	"testing"

	"github.com/abbott/hardn/pkg/interfaces"
)

func TestCheckFirewallStatus_Command(t *testing.T) {
	mockCmd := interfaces.NewMockCommander()
	mockCmd.CommandOutputs["ufw status verbose"] = []byte("Status: active\nDefault: deny (incoming) allow (outgoing)\nALLOW IN 22/tcp")
	enabled, configured := checkFirewallStatus(mockCmd)
	if !enabled || !configured {
		t.Fatalf("unexpected status: %v %v", enabled, configured)
	}
	if len(mockCmd.ExecutedCommands) != 1 || mockCmd.ExecutedCommands[0] != "ufw status verbose" {
		t.Errorf("unexpected commands: %v", mockCmd.ExecutedCommands)
	}
}
