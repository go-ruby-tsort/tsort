// Copyright (c) the go-ruby-tsort/tsort authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package tsort is a pure-Go (CGO=0), MRI-faithful port of Ruby's TSort stdlib:
// topological sorting and strongly connected components over an arbitrary directed
// graph, using Tarjan's algorithm exactly as MRI's tsort.rb does.
//
// A graph is described functionally by two callbacks, mirroring Ruby's
// tsort_each_node / tsort_each_child:
//
//   - nodes(yield)          yields every node in the graph.
//   - children(node, yield) yields every child (out-edge target) of node.
//
// Both use the Go range-function shape func(yield func(any)). Nodes are arbitrary
// any values; equality between two nodes is decided by an Identity function
// (mirroring Ruby's eql?/hash). When no Identity is supplied the nodes are used as
// Go map keys directly, which requires them to be comparable.
//
// The traversal order, component grouping and component ordering all match MRI
// byte-for-byte: each SCC is emitted in reverse topological order (children before
// parents), and a component's internal order is its DFS discovery order.
package tsort

import (
	"fmt"
	"strings"
)

// NodesFunc yields every node of the graph, mirroring Ruby's tsort_each_node.
type NodesFunc = func(yield func(any))

// ChildrenFunc yields every child of node, mirroring Ruby's tsort_each_child.
type ChildrenFunc = func(node any, yield func(any))

// Cyclic is raised (returned) when a topological sort encounters a strongly
// connected component of more than one node, i.e. a true cycle. Its Error string
// reproduces MRI's message exactly: "topological sort failed: [...]" where the
// bracketed part is the Ruby #inspect of the offending component.
type Cyclic struct {
	// Component is the strongly connected component (length >= 2) that broke the
	// sort, in the same order MRI would have yielded it.
	Component []any
	msg       string
}

func (c *Cyclic) Error() string { return c.msg }

// Options tunes how nodes are identified and inspected so that the MRI-faithful
// behaviour can be reproduced for any host node representation (e.g. rbgo's boxed
// Ruby objects). The zero Options work for comparable Go nodes inspected with a
// Go-side approximation of Ruby #inspect.
type Options struct {
	// Identity maps a node to a comparable key used for graph-equality (Ruby's
	// eql?/hash). If nil, the node itself is used as the key, which means nodes
	// must be comparable Go values.
	Identity func(node any) any

	// Inspect renders a node the way Ruby's #inspect would, used only to build the
	// Cyclic error message. If nil, a built-in approximation is used.
	Inspect func(node any) string
}

func (o *Options) identity(node any) any {
	if o != nil && o.Identity != nil {
		return o.Identity(node)
	}
	return node
}

func (o *Options) inspectComponent(component []any) string {
	insp := rubyInspect
	if o != nil && o.Inspect != nil {
		insp = o.Inspect
	}
	parts := make([]string, len(component))
	for i, n := range component {
		parts[i] = insp(n)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// TSort returns a topologically sorted slice of nodes, sorted from children to
// parents: the first element has no child and the last has no parent. It mirrors
// Ruby's TSort.tsort.
//
// If the graph contains a cycle (an SCC with more than one node) a *Cyclic error
// is returned. A self-loop forms a single-node SCC and therefore — exactly like
// MRI — does NOT raise.
func TSort(nodes NodesFunc, children ChildrenFunc) ([]any, error) {
	return TSortWith(nodes, children, nil)
}

// TSortWith is TSort with explicit Options.
func TSortWith(nodes NodesFunc, children ChildrenFunc, opts *Options) ([]any, error) {
	var result []any
	var cyclic *Cyclic
	EachStronglyConnectedComponentWith(nodes, children, opts, func(component []any) {
		if cyclic != nil {
			return
		}
		if len(component) == 1 {
			result = append(result, component[0])
			return
		}
		cyclic = &Cyclic{
			Component: component,
			msg:       "topological sort failed: " + opts.inspectComponent(component),
		}
	})
	if cyclic != nil {
		return nil, cyclic
	}
	if result == nil {
		result = []any{}
	}
	return result, nil
}

// StronglyConnectedComponents returns the strongly connected components as a slice
// of slices, sorted from children to parents (reverse topological order). Mirrors
// Ruby's TSort.strongly_connected_components.
func StronglyConnectedComponents(nodes NodesFunc, children ChildrenFunc) [][]any {
	return StronglyConnectedComponentsWith(nodes, children, nil)
}

// StronglyConnectedComponentsWith is StronglyConnectedComponents with explicit Options.
func StronglyConnectedComponentsWith(nodes NodesFunc, children ChildrenFunc, opts *Options) [][]any {
	result := [][]any{}
	EachStronglyConnectedComponentWith(nodes, children, opts, func(component []any) {
		result = append(result, component)
	})
	return result
}

// EachStronglyConnectedComponent invokes yield for each strongly connected
// component, in reverse topological order. Mirrors Ruby's
// TSort.each_strongly_connected_component.
func EachStronglyConnectedComponent(nodes NodesFunc, children ChildrenFunc, yield func(component []any)) {
	EachStronglyConnectedComponentWith(nodes, children, nil, yield)
}

// EachStronglyConnectedComponentWith is EachStronglyConnectedComponent with
// explicit Options.
func EachStronglyConnectedComponentWith(nodes NodesFunc, children ChildrenFunc, opts *Options, yield func(component []any)) {
	st := newState(children, opts, yield)
	nodes(func(node any) {
		key := opts.identity(node)
		if _, seen := st.idMap[key]; !seen {
			st.from(node, key)
		}
	})
}

// EachStronglyConnectedComponentFrom iterates over the strongly connected
// components in the subgraph reachable from start. It does NOT enumerate the node
// set (no NodesFunc), exactly like Ruby's
// TSort.each_strongly_connected_component_from. Mirrors that method.
func EachStronglyConnectedComponentFrom(start any, children ChildrenFunc, yield func(component []any)) {
	EachStronglyConnectedComponentFromWith(start, children, nil, yield)
}

// EachStronglyConnectedComponentFromWith is EachStronglyConnectedComponentFrom
// with explicit Options.
func EachStronglyConnectedComponentFromWith(start any, children ChildrenFunc, opts *Options, yield func(component []any)) {
	st := newState(children, opts, yield)
	st.from(start, opts.identity(start))
}

// state carries the mutable bookkeeping of the recursive Tarjan traversal, kept in
// lock-step with MRI's each_strongly_connected_component_from.
type state struct {
	children ChildrenFunc
	opts     *Options
	yield    func([]any)

	// idMap maps a node identity to its DFS index. A settled node (its SCC already
	// emitted) is remapped to the sentinel value -1, mirroring MRI's id_map[n]=nil:
	// such a node is "included" yet contributes no low-link.
	idMap map[any]int
	// node remembers the original (un-keyed) node for each identity, so emitted
	// components carry the caller's node values, not their identity keys.
	node  map[any]any
	stack []any
}

// settled is the sentinel for an SCC-settled node, mirroring MRI's id_map[n]=nil.
const settled = -1

func newState(children ChildrenFunc, opts *Options, yield func([]any)) *state {
	return &state{
		children: children,
		opts:     opts,
		yield:    yield,
		idMap:    map[any]int{},
		node:     map[any]any{},
	}
}

// from is the faithful port of MRI's
// each_strongly_connected_component_from(node, each_child, id_map, stack).
// It returns the low-link (minimum_id) of node.
func (s *state) from(node, key any) int {
	nodeID := len(s.idMap)
	minimumID := nodeID
	s.idMap[key] = nodeID
	s.node[key] = node
	stackLength := len(s.stack)
	s.stack = append(s.stack, node)

	s.children(node, func(child any) {
		childKey := s.opts.identity(child)
		if childID, seen := s.idMap[childKey]; seen {
			// childID == settled (MRI nil) means already in a finished SCC; skip
			// it, matching MRI's `if child_id && child_id < minimum_id`.
			if childID != settled && childID < minimumID {
				minimumID = childID
			}
		} else {
			subMinimumID := s.from(child, childKey)
			if subMinimumID < minimumID {
				minimumID = subMinimumID
			}
		}
	})

	if nodeID == minimumID {
		component := append([]any(nil), s.stack[stackLength:]...)
		s.stack = s.stack[:stackLength]
		for _, n := range component {
			s.idMap[s.opts.identity(n)] = settled
		}
		s.yield(component)
	}

	return minimumID
}

// rubyInspect renders a node roughly the way Ruby's Object#inspect would, covering
// the value kinds tsort is realistically asked to sort (the numeric tower, strings,
// symbols, booleans, nil and arrays of the same). It is only used to build the
// Cyclic error string; hosts with their own node objects supply Options.Inspect.
func rubyInspect(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return rubyInspectString(x)
	case Symbol:
		return ":" + string(x)
	case fmt.Stringer:
		return x.String()
	case int:
		return fmt.Sprintf("%d", x)
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", x)
	case float32:
		return rubyInspectFloat(float64(x))
	case float64:
		return rubyInspectFloat(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = rubyInspect(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// Symbol is a node value that inspects as a Ruby symbol (":name"). rbgo binds its
// own Ruby Symbol; this type lets the library's own tests reproduce MRI's symbol
// rendering in Cyclic messages.
type Symbol string

func rubyInspectString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func rubyInspectFloat(f float64) string {
	// Ruby renders integral floats with a trailing ".0" (e.g. 2.0), which Go's %v
	// also does for float64; reuse strconv-equivalent formatting via fmt and patch
	// the integral case.
	s := fmt.Sprintf("%v", f)
	if !strings.ContainsAny(s, ".eEnN") { // no dot, exponent, Inf/NaN marker
		s += ".0"
	}
	return s
}
