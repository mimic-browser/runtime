// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

// selKey parses src as a single selector and returns it, failing the test if
// parsing drops it entirely (a nil/empty result).
func selKey(t *testing.T, src string) Selector {
	t.Helper()
	sels := ParseSelectorList(src)
	if len(sels) != 1 {
		t.Fatalf("ParseSelectorList(%q) = %d selectors, want 1", src, len(sels))
	}
	return sels[0]
}

func TestHostBareOnlyMatchesTheBoundHost(t *testing.T) {
	sel := selKey(t, ":host")
	host := el("my-elem", "", "")
	other := el("my-elem", "", "") // same tag, different node identity
	if !sel.MatchesHost(host, host) {
		t.Error(":host should match the bound host")
	}
	if sel.MatchesHost(other, host) {
		t.Error(":host should not match a different node, even same tag")
	}
	// Matches(n) == MatchesHost(n, nil): outside any shadow scope there is no
	// host to bind, so a bare ":host" never matches.
	if sel.Matches(host) {
		t.Error(":host must never match via plain Matches (no host bound)")
	}
}

func TestHostFunctionalArgumentMustAlsoMatchHost(t *testing.T) {
	sel := selKey(t, ":host(.open)")
	host := el("my-elem", "", "")
	if sel.MatchesHost(host, host) {
		t.Error(":host(.open) matched a host with no 'open' class")
	}
	host.Attr["class"] = "open"
	if !sel.MatchesHost(host, host) {
		t.Error(":host(.open) should match once the host carries the class")
	}
}

func TestHostWithNotArgument(t *testing.T) {
	// The real idiom both confirmed sites use: hide slotted content until an
	// attribute-driven "open" state is toggled.
	sel := selKey(t, ":host(:not([open])) slot")
	host := el("my-elem", "", "")
	slot := el("slot", "", "")
	// slot sits at the top of its shadow tree: attachDeclarativeShadowRoots
	// nils out a top-level shadow node's Parent (see dom/shadow.go), which is
	// exactly the boundary condition matchLeft's ":host" escape depends on.
	slot.Parent = nil

	if !sel.MatchesHost(slot, host) {
		t.Error("slot should be selected while host lacks [open]")
	}
	host.Attr["open"] = ""
	if sel.MatchesHost(slot, host) {
		t.Error("slot should NOT be selected once host carries [open]")
	}
}

func TestHostDoesNotLeakPastTheShadowBoundary(t *testing.T) {
	// A plain (non-":host") ancestor compound must never reach past the
	// shadow tree's top into the host's own light-DOM ancestry, even though
	// the host itself is reachable via the sanctioned ":host" escape.
	sel := selKey(t, "body slot")
	host := el("my-elem", "", "")
	body := el("body", "", "")
	host.Parent = body // host's OWN real light-DOM ancestor
	slot := el("slot", "", "")
	slot.Parent = nil // top of the shadow tree — see above

	if sel.MatchesHost(slot, host) {
		t.Error("\"body slot\" must not match across the shadow boundary")
	}
}

func TestHostCompoundNeverMatchesViaPlainMatches(t *testing.T) {
	// Defensive guard: a Host compound reached through a context with no host
	// binding (e.g. as a ":not()" argument, or ordinary querySelector) must
	// never match — there is nothing sensible to compare it against.
	c := compound{Host: true}
	n := el("my-elem", "", "")
	if c.matches(n) {
		t.Error("a Host compound must never match via plain matches()")
	}
	if c.matchesHost(n, nil) {
		t.Error("a Host compound must not match with a nil host")
	}
	if !c.matchesHost(n, n) {
		t.Error("a bare Host compound should match when n IS host")
	}
}

func TestHostSpecificity(t *testing.T) {
	bare := selKey(t, ":host")
	withArg := selKey(t, ":host(.foo)")
	withNot := selKey(t, ":host(:not(.foo))")
	if bare.Specificity() == 0 {
		t.Error(":host alone should carry pseudo-class (class-level) specificity")
	}
	if withArg.Specificity() <= bare.Specificity() {
		t.Errorf(":host(.foo) specificity %d should exceed bare :host %d", withArg.Specificity(), bare.Specificity())
	}
	// :host(:not(.foo)) contributes :not's argument's specificity, same shape
	// as a bare :not(.foo) would, on top of :host's own pseudo-class weight.
	if withNot.Specificity() != withArg.Specificity() {
		t.Errorf(":host(:not(.foo)) specificity %d, want %d (same as :host(.foo))", withNot.Specificity(), withArg.Specificity())
	}
}

func TestHostContextIsUnmodelledAndSafelyDropped(t *testing.T) {
	// A compound that is ONLY ":host-context(...)" has nothing left once the
	// (unmodelled) pseudo is ignored, so parseSimple's "reduces to nothing"
	// check fails it — which drops the WHOLE selector (see parseComplex),
	// never silently "matches everything".
	if sels := ParseSelectorList(":host-context(.dark)"); len(sels) != 0 {
		t.Errorf("bare :host-context(...) should be dropped entirely, got %+v", sels)
	}
	// Attached to a real tag/class, the surrounding constraint survives and
	// the unmodelled :host-context part is silently ignored (reduce, don't
	// drop) — ordinary behaviour for any unknown pseudo, not special-cased.
	sel := selKey(t, "button:host-context(.dark)")
	if len(sel.parts) != 1 || sel.parts[0].Tag != "button" || sel.parts[0].Host {
		t.Errorf("button:host-context(.dark) = %+v, want a plain 'button' compound", sel.parts)
	}
}

func TestHostSelectorAlternativeThatIsAlwaysFalseContributesNothing(t *testing.T) {
	// ":host(:hover)" — :hover can never match statically; parseSimpleSelectorList
	// drops it (mirroring :not()'s handling of the same case), so the
	// argument list ends up empty and ":host(:hover)" behaves like bare
	// ":host" would if the argument were absent... except HostSelector isn't
	// nil here (arg was non-empty), it's an EMPTY non-nil-vs-nil distinction
	// that must not accidentally make it match everything: the loop over zero
	// alternatives finds none, so it must fail to match, not vacuously pass.
	sel := selKey(t, ":host(:hover)")
	host := el("my-elem", "", "")
	if sel.MatchesHost(host, host) {
		t.Error(":host(:hover) should never match (its only alternative is dynamic/always-false)")
	}
}
