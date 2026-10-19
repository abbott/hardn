package sshd

import (
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
)

// TestSocketComplete: SocketComplete holds only when every line systemctl
// show -p Listen printed was read, whether sshd -T's output was read or
// not (p2-sshd-migrate refuses to hold a socket set that is not).
func TestSocketComplete(t *testing.T) {
	ssh := detect.SSH{Mode: detect.SSHSocket, Socket: detect.UnitState{Active: "active"}}
	const t22 = "port 22\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\n"
	for _, tt := range []struct {
		name, effective, socket string
		complete                bool
		n                       int
	}{
		{"read", t22, "Listen=[::]:22 (Stream)\n", true, 1},
		{"empty output", t22, "", true, 0},
		{"empty property", t22, "Listen=\n", true, 0},
		{"a trailing space", t22, "Listen=[::]:22 (Stream) \n", false, 0},
		{"one of two unread", t22, "Listen=[::]:22 (Stream)\nListen=/run/ssh.sock (Stream)\n", false, 1},
		{"sshd -T unread", "", "Listen=[::]:22 (Datagram)\n", false, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := Evaluate(EffectiveInputs{Effective: []byte(tt.effective), SSH: ssh, Socket: []byte(tt.socket)})
			if l := e.Listeners; !l.SocketRead || l.SocketComplete != tt.complete || len(l.Socket) != tt.n {
				t.Errorf("SocketRead %v, SocketComplete %v, Socket %v; want true, %v, %d entries", l.SocketRead, l.SocketComplete, l.Socket, tt.complete, tt.n)
			}
		})
	}
	if e, _ := Evaluate(EffectiveInputs{Effective: []byte(t22), SSH: detect.SSH{Mode: detect.SSHService}}); e.Listeners.SocketComplete {
		t.Error("SocketComplete without SocketRead")
	}
}
