package sshd_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/sshd"
)

// TestReadOrder: the drop-ins sshd reads, in C byte order, at the Include;
// aliases as their keyword; Match blocks ending with their file.
func TestReadOrder(t *testing.T) {
	main := "# c\n" + sshd.IncludeLine + "\nPort 22\nMatch User bob\n  PasswordAuthentication yes\n"
	drops := []sshd.File{
		{Path: sshd.DropInDir + "/50-cloud-init.conf", Data: []byte("PasswordAuthentication yes\n")},
		{Path: sshd.DropInDir + "/00-hardn.conf", Data: []byte("HostbasedAcceptedKeyTypes ssh-ed25519\nMatch User alice\n  X11Forwarding no\n")},
		{Path: sshd.DropInDir + "/.x.conf", Data: []byte("Port 1\n")},
		{Path: "/etc/ssh/other.conf", Data: []byte("Port 2\n")},
		{Path: sshd.DropInDir + "/z.conf", Data: []byte("Subsystem sftp internal-sftp\nAuthorizedKeysFile \"a b\" c\n")},
	}
	var got []string
	for _, l := range sshd.ReadOrder([]byte(main), drops) {
		m := ""
		if l.Match != nil {
			m = " under " + l.Match.Text
		}
		got = append(got, fmt.Sprintf("%s:%d %s %q%s", l.File[strings.LastIndex(l.File, "/")+1:], l.Line, l.Keyword, l.Args, m))
	}
	want := []string{
		`00-hardn.conf:1 hostbasedacceptedalgorithms ["ssh-ed25519"]`,
		`00-hardn.conf:3 x11forwarding ["no"] under Match User alice`,
		`50-cloud-init.conf:1 passwordauthentication ["yes"]`,
		`z.conf:1 subsystem sftp ["sftp" "internal-sftp"]`,
		`z.conf:2 authorizedkeysfile ["a b" "c"]`,
		`sshd_config:3 port ["22"]`,
		`sshd_config:5 passwordauthentication ["yes"] under Match User bob`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("ReadOrder:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if ls := sshd.ReadOrder([]byte("Port 22\n"), drops); len(ls) != 1 {
		t.Errorf("without an Include, the drop-ins are read: %v", ls)
	}
}

func TestAccumulates(t *testing.T) {
	for k, want := range map[string]bool{"Port": true, "listenaddress": true, "AllowUsers": true, "HostDSAKey": true,
		"PermitRootLogin": false, "AuthorizedKeysFile": false, "Subsystem": false} {
		if got := sshd.Accumulates(k); got != want {
			t.Errorf("Accumulates(%q) = %v", k, got)
		}
	}
	if got := sshd.CanonicalKeyword("ChallengeResponseAuthentication", []string{"no"}); got != "kbdinteractiveauthentication" {
		t.Errorf("CanonicalKeyword(ChallengeResponseAuthentication) = %q", got)
	}
	if got := sshd.CanonicalKeyword("Subsystem", []string{"sftp", "x"}); got != "subsystem sftp" {
		t.Errorf("CanonicalKeyword(Subsystem sftp) = %q", got)
	}
}
