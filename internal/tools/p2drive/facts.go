package main

import (
	"context"
	"flag"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// setupFacts is the read-only report: what each primitive reads on this
// host, against the tables of docs/safety/*.md §4. It changes nothing; a
// read that fails is printed and the report goes on.
func setupFacts(*flag.FlagSet) func(context.Context, *driver) error {
	return func(ctx context.Context, d *driver) error {
		f, err := d.detect(ctx, d.sys)
		if err != nil {
			return err
		}
		d.say("os-release: %s %s %q (%s), like %v; err %v", f.OS.ID, f.OS.VersionID, f.OS.VersionCodename, f.OS.Path, f.OS.IDLike, f.OS.Err)
		d.say("platform: %s %s, %v %s; err %v", f.Platform.Distro, f.Platform.Version, f.Platform.Support, f.Platform.Note, f.Platform.Err)
		d.say("init: %v, running %v (%s); err %v", f.Init.Kind, f.Init.Running, f.Init.Source, f.Init.Err)
		d.say("ssh: %v; ssh.socket %s/%s, ssh.service %s/%s; generator %v %s; openrc started %v, runlevels %v; err %v",
			f.SSH.Mode, f.SSH.Socket.Enabled, f.SSH.Socket.Active, f.SSH.Service.Enabled, f.SSH.Service.Active,
			f.SSH.Generator, f.SSH.GeneratorPath, f.SSH.OpenRC.Started, f.SSH.OpenRC.Runlevels, f.SSH.Err)
		d.say("proxmox: installed %v %s (%s), pmxcfs %v %s, /etc/pve %v; err %v", f.Proxmox.Installed, f.Proxmox.Version, f.Proxmox.Source, f.Proxmox.ClusterFS, f.Proxmox.ClusterFSType, f.Proxmox.EtcPVE, f.Proxmox.Err)
		d.say("container: %v %s %s; err %v", f.Container.Detected, f.Container.Signal, f.Container.Manager, f.Container.Err)
		d.say("elevation: sudo %v %s (%v %s), doas %v %s; err %v", f.Elevation.Sudo.Present, f.Elevation.Sudo.Path, f.Elevation.SudoFlavor, f.Elevation.SudoVersion, f.Elevation.Doas.Present, f.Elevation.Doas.Path, f.Elevation.Err)

		if inc, err := sshd.DetectInclude(ctx, d.sys); err != nil {
			d.say("include: %v", err)
		} else {
			d.say("include: %v, line %d %q", inc.Mode, inc.Line, inc.Text)
		}
		found, err := migrate.Find(ctx, d.sys, migrate.FindRequest{})
		if err != nil {
			d.say("legacy SSH files: %v", err)
		}
		for _, a := range found.Artifacts {
			d.say("legacy: %v (%v) %s", a.Kind, a.Gen, a.Path)
		}
		binary := ""
		if f.Init.Kind == detect.OpenRC {
			if binary, err = reload.OpenRCFlavour(ctx, d.sys); err != nil {
				d.say("openrc sshd binary: %v", err)
			} else {
				d.say("openrc sshd binary: %s", binary)
			}
		}
		eff, findings, err := sshd.ReadEffective(ctx, d.sys, sshd.EffectiveRequest{SSH: f.SSH, Binary: binary})
		if err != nil {
			d.say("sshd effective: %v", err)
		} else {
			d.sayEffective("now", eff, findings)
			d.say("conflicts (known %v):", eff.ConflictsKnown)
			for _, c := range eff.Conflicts {
				d.say("  %v %s: %s:%d %q (hardn %s:%d)", c.Kind, c.Keyword, c.Other.File, c.Other.Line, c.Other.Text, c.Hardn.File, c.Hardn.Line)
			}
		}
		if sshd.SocketMayListen(f.SSH) {
			drops, err := reload.ReadSocketDropIns(ctx, d.sys)
			d.say("ssh.socket drop-ins (read %v): %v; err %v", drops.Read, drops.Files, err)
		}
		if bound, bad, err := reload.ReadBound(ctx, d.sys); err != nil {
			d.say("bound (ss -Htln): %v", err)
		} else {
			d.say("bound (ss -Htln): %v; unread %q", bound, bad)
		}

		if st, err := ufw.Read(ctx, d.sys, f.Proxmox); err != nil {
			d.say("ufw: %v", err)
		} else {
			d.say("ufw: installed %v, active %v, enabled %v, ipv6 %v, incoming %s (status %s), %d saved rules, unread %q",
				st.Installed, st.Status.Active, st.Enabled, st.IPv6, st.Incoming, st.Status.Incoming, len(st.Rules), st.Bad)
			if eff.Listeners.Known {
				cov, fds := ufw.Check(st, ufw.Needs(eff.Listeners, nil, nil))
				for _, c := range cov {
					d.say("  %s %v %d: %v (%s)", c.Need.Why, c.Need.Family, c.Need.Port, c.Verdict, c.By)
				}
				for _, x := range fds {
					d.say("  finding %v", x)
				}
			}
		}

		if ok, _ := pve.Applies(f.Proxmox); ok {
			groups, gerr := pve.RootGroups(ctx, d.sys)
			chk, err := pve.CheckSSH(f.Proxmox, pve.SSHInput{Effective: eff, Main: found.Main, DropIns: found.DropIns, Root: groups})
			d.say("proxmox ssh: root key-only %v, root by key %v, port 22 %v; err %v (groups %v)", chk.RootKeyOnly, chk.RootByKey, chk.Port22, err, gerr)
			for _, x := range chk.Findings {
				d.say("  %v: %s:%d %s", x.Kind, x.File, x.Line, x.Detail)
			}
			fw, err := pve.ReadFirewall(ctx, d.sys, f.Proxmox)
			d.say("pve-firewall: enabled %v, policy_in %q, nftables %v, %s %s, management %v, ssh %v, web %v; err %v %v",
				fw.Enabled, fw.PolicyIn, fw.NFTables, fw.Unit, fw.Service.Active, fw.Management, fw.SSH, fw.Web, fw.Err, err)
			if k, err := pve.ReadKeys(ctx, d.sys, f.Proxmox); err != nil {
				d.say("root's keys: %v", err)
			} else {
				d.say("root's keys: %s -> %s, nodes %v", pve.RootKeys, k.Target, k.Nodes)
			}
		}

		c := sudoers.Choose(f, sudoers.ToolUnknown)
		d.say("sudoers: %v (flavour %v); err %v", c.Tool, c.Flavor, c.Err)

		st, err := revert.ReadState(ctx, d.sys, revert.Options{})
		if err != nil {
			d.say("journal: %v", err)
		}
		for _, n := range revert.Notices(st, d.now()) {
			d.say("journal %s: %s", n.Kind, n.Text)
		}
		return nil
	}
}
