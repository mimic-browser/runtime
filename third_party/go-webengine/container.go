// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// maxContainerPasses bounds how many times the container-query resolution
// loop (gatherContainerSizes -> re-cascade -> re-layout) runs before the
// result is accepted as final. It exists for the same reason as dynamic.go's
// maxSettlePasses: a container's own size can in principle depend on a
// descendant that is itself conditioned by a DIFFERENT @container rule
// (nested containers), so one pass is not always enough to reach a fixpoint —
// but the loop must still be guaranteed to terminate. Real pages settle in
// one or two passes; this cap guarantees termination regardless.
const maxContainerPasses = 4

// layoutWithContainers resolves @container queries against real geometry by
// iterating cascade -> layout -> measure -> re-cascade to a bounded fixpoint,
// starting from sm/box/height (an already-computed cascade+layout pass, in
// which every @container rule was necessarily inactive — CascadeVW, which
// callers use for that first pass, always passes a nil container-size map).
//
// This plays exactly the role dynamic.go's settle loop plays for JavaScript
// geometry feedback (settle: cascade -> layout -> run scripts -> re-cascade
// -> re-layout to a bounded fixpoint) but for a different chicken-and-egg
// problem: a @container condition depends on an ancestor ELEMENT's actual
// laid-out size, which isn't known until layout has run at least once,
// whereas @media's viewport-width condition is known up front. So where
// @media is resolved once in CascadeVW, @container needs this loop: measure
// the current layout's query containers, re-cascade with that information,
// re-layout, and repeat until no container's measured size changes (or the
// pass cap is hit, in which case the last computed pass is accepted as-is —
// never hang, never loop unboundedly, same discipline as the settle loop).
//
// If nothing in the tree establishes a query container at all (the common
// case: no `container-type` anywhere), sm/box/height are returned unchanged
// on the first check, at the cost of one BuildIndex walk — cheap, and it
// keeps this function safe to call unconditionally from every layout call
// site rather than requiring each caller to pre-check for container-type.
func layoutWithContainers(root *dom.Node, sheets []string, vw, vh float64, m layout.Measurer, imgSize map[*dom.Node][2]float64, sm css.StyleMap, box *layout.Box, height float64) (css.StyleMap, *layout.Box, float64) {
	var prevSizes map[*dom.Node]css.ContainerSize
	for pass := 0; pass < maxContainerPasses; pass++ {
		sizes := gatherContainerSizes(sm, layout.BuildIndex(box))
		if len(sizes) == 0 {
			break // nothing establishes a query container: no @container rule can ever match
		}
		if containerSizesEqual(sizes, prevSizes) {
			break // fixpoint: another pass would recompute the identical sizes
		}
		prevSizes = sizes
		sm = css.CascadeVWContainers(root, vw, sheets, sizes)
		box, height = layout.LayoutDocumentViewport(root, sm, vw, vh, m, imgSize)
	}
	return sm, box, height
}

// gatherContainerSizes walks sm for every element whose computed style
// establishes a query container (container-type != normal) and looks up its
// border-box size in idx (the just-completed layout pass' node->rect index,
// from layout.BuildIndex). An element with container-type set that never got
// a box (display:none, or simply absent from this layout) is skipped — it
// contributes no size for @container to consult, which correctly falls back
// to "not yet known" (satisfied's fail-closed default) rather than a stale or
// synthesised value.
//
// InlineSize is always the container's border-box width; BlockSize (border-
// box height) is only measured for a ContainerTypeSize container, matching
// what ContainerCondition.satisfied ever consults (see css/container.go).
func gatherContainerSizes(sm css.StyleMap, idx map[*dom.Node]layout.Rect) map[*dom.Node]css.ContainerSize {
	var out map[*dom.Node]css.ContainerSize
	for n, st := range sm {
		if st.ContainerType == css.ContainerNormal {
			continue
		}
		r, ok := idx[n]
		if !ok {
			continue
		}
		if out == nil {
			out = map[*dom.Node]css.ContainerSize{}
		}
		cs := css.ContainerSize{InlineSize: r.W}
		if st.ContainerType == css.ContainerTypeSize {
			cs.BlockSize = r.H
		}
		out[n] = cs
	}
	return out
}

// containerSizesEqual reports whether two container-size snapshots are
// identical (same set of container nodes, same sizes for each) — the
// fixpoint test layoutWithContainers uses to stop iterating once another pass
// would recompute nothing new.
func containerSizesEqual(a, b map[*dom.Node]css.ContainerSize) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok || bv != v {
			return false
		}
	}
	return true
}
