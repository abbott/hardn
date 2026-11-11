package engine

import "testing"

// TestSamePlanVolatile: a Volatile finding (other programs' sockets, read
// again under the run lock) coming, going or changing leaves the plan the
// same; any other finding's change makes it a changed plan.
func TestSamePlanVolatile(t *testing.T) {
	plan := func(fs ...Finding) *Plan {
		return &Plan{Changes: []Change{{ID: "web-01/firewall", Summary: "enable ufw", Findings: fs}}}
	}
	vol := Finding{Code: "FIREWALL_PORTS_BLOCKED", Detail: "ufw will block 8000/tcp.", Volatile: true}
	vol2 := Finding{Code: "FIREWALL_PORTS_BLOCKED", Detail: "ufw will block 9000/tcp.", Volatile: true}
	ssh := Finding{Code: "SSH_NOT_IN_EFFECT", Detail: "sshd listens on 22, not 2230"}
	for _, tc := range []struct {
		name  string
		shown *Plan
		fresh *Plan
		want  bool
	}{
		{"none", plan(), plan(), true},
		{"volatile appears", plan(), plan(vol), true},
		{"volatile changes", plan(vol), plan(vol2), true},
		{"volatile goes, the other stays", plan(ssh, vol), plan(ssh), true},
		{"the other changes", plan(ssh), plan(Finding{Code: ssh.Code, Detail: "sshd listens on 2222, not 2230"}), false},
		{"the other appears", plan(vol), plan(vol, ssh), false},
		{"the other goes", plan(ssh), plan(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := samePlan(tc.shown, tc.fresh); got != tc.want {
				t.Errorf("samePlan = %v, want %v", got, tc.want)
			}
		})
	}
}
