// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"fmt"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/bll/clusterupgrade"
)

// planLine is one printed line of a step. A charts step prints one line per
// chart; only its first line carries the step number and the note.
type planLine struct {
	step     int // 0 on a continuation line
	name     string
	from, to string
	note     string
}

// renderPlan is the text form of a plan: the steps numbered in order under a
// heading per phase, with [next] on step 1 and the columns aligned.
func renderPlan(plan clusterupgrade.Plan) string {
	var b strings.Builder
	target := fmt.Sprintf("Kubernetes %s, Cilium %s", plan.Target.Kubernetes, plan.Target.Cilium)

	if len(plan.Blockers) > 0 {
		fmt.Fprintf(&b, "Cannot plan the upgrade to Kubernetes %s:\n", plan.Target.Kubernetes)
		for _, bl := range plan.Blockers {
			fmt.Fprintf(&b, "  - %s: %s\n", bl.Code, bl.Detail)
		}
		return b.String()
	}
	if len(plan.Hops) == 0 {
		fmt.Fprintf(&b, "Nothing to do: the cluster is at or past the target (%s).\n", target)
		return b.String()
	}

	sections := make([]string, len(plan.Hops))
	lines := make([][]planLine, len(plan.Hops))
	nameW, fromW, toW := 0, 0, 0
	for i, h := range plan.Hops {
		sections[i] = sectionOf(h)
		lines[i] = linesOf(i+1, h)
		for _, l := range lines[i] {
			nameW, fromW, toW = max(nameW, len(l.name)), max(fromW, len(l.from)), max(toW, len(l.to))
		}
	}

	fmt.Fprintf(&b, "Cluster upgrade plan: %d %s remaining (target: %s)\n", len(plan.Hops), plural(len(plan.Hops), "step", "steps"), target)
	for i := range plan.Hops {
		if i == 0 || sections[i] != sections[i-1] {
			fmt.Fprintf(&b, "\n  %s:\n", sections[i])
		}
		for _, l := range lines[i] {
			marker, number := "      ", "   "
			if l.step == 1 {
				marker = "[next]"
			}
			if l.step > 0 {
				number = fmt.Sprintf("%2d.", l.step)
			}
			row := fmt.Sprintf("  %s %s %-*s  %-*s -> %-*s", marker, number, nameW, l.name, fromW, l.from, toW, l.to)
			if l.note != "" {
				row += "  (" + l.note + ")"
			}
			fmt.Fprintln(&b, strings.TrimRight(row, " "))
		}
	}
	return b.String()
}

func sectionOf(h clusterupgrade.Hop) string {
	switch {
	case h.Step == clusterupgrade.StepResume:
		return "Interrupted Kubernetes upgrade"
	case h.Kind == clusterupgrade.KindStructural:
		return "Structural migrations"
	case h.Step == clusterupgrade.StepRefresh:
		return "Charts refresh"
	default:
		return "Version ladder"
	}
}

func linesOf(step int, h clusterupgrade.Hop) []planLine {
	switch h.Kind {
	case clusterupgrade.KindKubernetes:
		note := string(h.Step)
		if h.Step == clusterupgrade.StepResume {
			note = "finish the kubelet"
		}
		return []planLine{{step, "Kubernetes", h.From, h.To, note}}
	case clusterupgrade.KindCilium:
		return []planLine{{step, "Cilium", h.From, h.To, string(h.Step)}}
	case clusterupgrade.KindStructural:
		note := "CRD API migration"
		if h.Step == clusterupgrade.StepReplace {
			note = "chart replacement"
		}
		return []planLine{{step, h.Component, h.From, h.To, note}}
	}

	note := "catalog default"
	if h.Step == clusterupgrade.StepCoupled {
		note = "needed by Kubernetes " + h.Kubernetes
	}
	out := make([]planLine, len(h.Charts))
	for i, c := range h.Charts {
		out[i] = planLine{name: c.Chart, from: c.From, to: c.To}
	}
	if len(out) > 0 {
		out[0].step, out[0].note = step, note
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
