// Package plans registers the plan rollbacks of p2-revert's journal: the
// units whose undo is their own checked plan rather than a backup, each
// journalled as one revert.OpPlan step (refactor plan P2,
// next/p2-revert-plans; docs/safety/revert.md §7). They are root's keys on
// Proxmox VE (pve.RollbackKeys), the sudo and doas rules
// (sudoers.Rollback), DNS (dns.Rollback), Alpine's updates
// (alpine.Rollback), and on Rocky Linux and AlmaLinux the SSH ports let
// through firewalld (firewalld.Rollback) and labelled for sshd
// (selinux.RollbackPorts).
//
// PVEKeys, Sudoers, DNS and Alpine make a unit's step: the plan, checked
// by the unit's own guard (its Check method), as JSON. Registry is
// revert.Options.Plans: for each unit, the function a rollback runs as
// root, from the journal alone and in another process. It decodes the
// step's JSON strictly (an unknown field, or anything after the document,
// is refused, as revert's journal is), runs the unit's guard again, and
// only then the unit's rollback. A step is decoded and checked that way
// before it is returned too, so a plan its rollback would refuse is never
// journalled.
//
// docs/safety/revert-plans.md describes the guarantees.
package plans

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/selinux"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The units, as revert.Step.Unit names them; each unit owns its name.
// Stable: a journal written by one hardn is rolled back by the next.
const (
	UnitPVEKeys = pve.KeysUnit // "pve-keys"
	UnitSudoers = sudoers.Unit // "sudoers"
	UnitDNS     = dns.Unit     // "dns"
	UnitAlpine  = alpine.Unit  // "alpine-updates"
	// UnitFirewalld and UnitSELinux are an SSH port change's on Rocky
	// Linux and AlmaLinux.
	UnitFirewalld = firewalld.Unit // "firewalld-ports"
	UnitSELinux   = selinux.Unit   // "selinux-ports"
)

// ErrPlan is a plan its unit's rollback does not accept: its JSON does not
// decode strictly into the unit's plan, or the unit's Check refuses it.
// The unit's own error is wrapped too (fs.ErrInvalid, pve.ErrPlanMismatch,
// pve.ErrBadKey).
var ErrPlan = errors.New("not a plan its unit's rollback accepts")

// kind is one unit's plan type, its guard and its rollback.
type kind[P any] struct {
	unit     string
	check    func(P) error
	rollback func(context.Context, host.System, P) error
}

var (
	pveKeys    = kind[pve.KeysPlan]{UnitPVEKeys, pve.KeysPlan.Check, pve.RollbackKeys}
	sudo       = kind[sudoers.Plan]{UnitSudoers, sudoers.Plan.Check, sudoers.Rollback}
	resolvers  = kind[dns.Plan]{UnitDNS, dns.Plan.Check, dns.Rollback}
	apkUpdates = kind[alpine.Plan]{UnitAlpine, alpine.Plan.Check, alpine.Rollback}
	fwPorts    = kind[firewalld.Undo]{UnitFirewalld, firewalld.Undo.Check, firewalld.Rollback}
	sePorts    = kind[selinux.PortsUndo]{UnitSELinux, selinux.PortsUndo.Check, selinux.RollbackPorts}
)

// Registry returns the plan rollbacks by unit, for revert.Options.Plans:
// Record refuses an OpPlan step of a unit that has none, and Rollback runs
// a step's through it. Each call returns a new map.
func Registry() map[string]revert.PlanRollback {
	return map[string]revert.PlanRollback{
		UnitPVEKeys:   pveKeys.rollbackJSON,
		UnitSudoers:   sudo.rollbackJSON,
		UnitDNS:       resolvers.rollbackJSON,
		UnitAlpine:    apkUpdates.rollbackJSON,
		UnitFirewalld: fwPorts.rollbackJSON,
		UnitSELinux:   sePorts.rollbackJSON,
	}
}

// PVEKeys journals a keys plan (pve.PlanKeys) as one OpPlan step, recorded
// before pve.ApplyKeys runs: an undo of a plan that never ran writes
// nothing (pve.UndoKeys). ApplyKeys' own backup stays out of
// Journal.Backup, whose whole-file restore would drop keys other nodes
// wrote meanwhile (docs/safety/pve.md §6). why is the step's line for the
// log.
func PVEKeys(p pve.KeysPlan, why string) (revert.Step, error) { return pveKeys.step(p, why) }

// Sudoers journals a sudoers or doas plan (sudoers.PlanFor) as one OpPlan
// step, recorded before sudoers.Apply runs: sudoers.Rollback puts back
// only a file that holds what the plan wrote.
func Sudoers(p sudoers.Plan, why string) (revert.Step, error) { return sudo.step(p, why) }

// DNS journals a DNS plan (dns.PlanDNS) as one OpPlan step. dns.Rollback
// cannot tell its own change from the same change made since, so it is
// for a plan whose Apply returned nil, or whose Apply hardn did not finish
// (docs/safety/dns.md §6).
func DNS(p dns.Plan, why string) (revert.Step, error) { return resolvers.step(p, why) }

// Alpine journals an Alpine updates plan (alpine.PlanUpdates) as one
// OpPlan step, under the same rule as DNS (docs/safety/alpine.md §6).
func Alpine(p alpine.Plan, why string) (revert.Step, error) { return apkUpdates.step(p, why) }

// Firewalld journals the ports a firewalld plan adds (firewalld.Plan.Undo)
// as one OpPlan step, recorded before firewalld.Execute runs:
// firewalld.Rollback takes away only a port that is there, which the plan
// found was not.
func Firewalld(u firewalld.Undo, why string) (revert.Step, error) { return fwPorts.step(u, why) }

// SELinuxPorts journals the ports a change labels ssh_port_t as one OpPlan
// step, recorded before semanage runs, under the same rule.
func SELinuxPorts(u selinux.PortsUndo, why string) (revert.Step, error) { return sePorts.step(u, why) }

// step checks p, encodes it, and decodes and checks the encoding as the
// rollback will, so that a plan that loses a field its rollback needs on
// the way through encoding/json is refused here, before it is journalled.
func (k kind[P]) step(p P, why string) (revert.Step, error) {
	if err := k.check(p); err != nil {
		return revert.Step{}, fmt.Errorf("plans: %s: %w, so it is not journalled: %w", k.unit, ErrPlan, err)
	}
	data, err := json.Marshal(p)
	if err != nil {
		return revert.Step{}, fmt.Errorf("plans: %s: %w", k.unit, err)
	}
	if _, err := k.decode(data); err != nil {
		return revert.Step{}, fmt.Errorf("%w (its round trip through JSON), so it is not journalled", err)
	}
	return revert.Step{Op: revert.OpPlan, Unit: k.unit, Plan: data, Why: why}, nil
}

// decode reads a journalled plan: one JSON document, every field one of
// the plan's, accepted by the unit's guard.
func (k kind[P]) decode(data []byte) (P, error) {
	var p P
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("plans: %s: %w: %w", k.unit, ErrPlan, err)
	}
	// Token, not More: More is false before a stray ']' or '}' too.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return p, fmt.Errorf("plans: %s: %w: data after the plan", k.unit, ErrPlan)
	}
	if err := k.check(p); err != nil {
		return p, fmt.Errorf("plans: %s: %w: %w", k.unit, ErrPlan, err)
	}
	return p, nil
}

// rollbackJSON is the unit's revert.PlanRollback: the plan decoded and
// checked, then the unit's rollback, under the rollback's context (already
// without cancellation, revert.Rollback).
func (k kind[P]) rollbackJSON(ctx context.Context, sys host.System, plan json.RawMessage) error {
	p, err := k.decode(plan)
	if err != nil {
		return fmt.Errorf("%w; refused, nothing undone", err)
	}
	return k.rollback(ctx, sys, p)
}
