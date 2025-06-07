package updates

import (
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/osdetect"
)

func TestUpdateSystem_Commands(t *testing.T) {
	mockCmd := interfaces.NewMockCommander()
	osInfo := &osdetect.OSInfo{OsType: "debian"}
	if err := UpdateSystem(osInfo, mockCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []string{"apt-get update", "apt-get upgrade -y"}
	if !reflect.DeepEqual(mockCmd.ExecutedCommands, expected) {
		t.Errorf("commands executed %v, expected %v", mockCmd.ExecutedCommands, expected)
	}
}
