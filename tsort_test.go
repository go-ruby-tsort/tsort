// Copyright (c) the go-ruby-tsort/tsort authors
//
// SPDX-License-Identifier: BSD-3-Clause

package tsort

import (
	"errors"
	"reflect"
	"testing"
)

// graph is an order-preserving adjacency list, standing in for the Ruby Hash that
// MRI's examples sort: node enumeration and child enumeration follow insertion
// order, so the traversal — and thus the output — matches MRI exactly.
type graph struct {
	order []any
	adj   map[any][]any
}

func newGraph() *graph { return &graph{adj: map[any][]any{}} }

func (g *graph) add(node any, children ...any) *graph {
	if _, ok := g.adj[node]; !ok {
		g.order = append(g.order, node)
	}
	g.adj[node] = append(g.adj[node], children...)
	return g
}

func (g *graph) nodes(yield func(any)) {
	for _, n := range g.order {
		yield(n)
	}
}

func (g *graph) children(node any, yield func(any)) {
	for _, c := range g.adj[node] {
		yield(c)
	}
}

func eq(a, b any) bool { return reflect.DeepEqual(a, b) }

func TestTSortSimpleDAG(t *testing.T) {
	// MRI: {1=>[2,3], 2=>[3], 3=>[], 4=>[]}.tsort #=> [3, 2, 1, 4]
	g := newGraph().add(1, 2, 3).add(2, 3).add(3).add(4)
	got, err := TSort(g.nodes, g.children)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []any{3, 2, 1, 4}; !eq(got, want) {
		t.Errorf("TSort = %v, want %v", got, want)
	}
}

func TestTSortDocGraph(t *testing.T) {
	// MRI: {1=>[2,3], 2=>[4], 3=>[2,4], 4=>[]}.tsort #=> [4, 2, 3, 1]
	g := newGraph().add(1, 2, 3).add(2, 4).add(3, 2, 4).add(4)
	got, err := TSort(g.nodes, g.children)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []any{4, 2, 3, 1}; !eq(got, want) {
		t.Errorf("TSort = %v, want %v", got, want)
	}
}

func TestTSortEmpty(t *testing.T) {
	g := newGraph()
	got, err := TSort(g.nodes, g.children)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []any{}; !eq(got, want) {
		t.Errorf("TSort(empty) = %v, want %v", got, want)
	}
}

func TestTSortSelfLoopDoesNotRaise(t *testing.T) {
	// MRI: {1=>[1]}.tsort #=> [1] (a self-loop is a size-1 SCC, so no Cyclic).
	g := newGraph().add(1, 1)
	got, err := TSort(g.nodes, g.children)
	if err != nil {
		t.Fatalf("self-loop must not raise, got: %v", err)
	}
	if want := []any{1}; !eq(got, want) {
		t.Errorf("TSort(self-loop) = %v, want %v", got, want)
	}
}

func TestTSortCyclic(t *testing.T) {
	// MRI: {1=>[2], 2=>[3,4], 3=>[2], 4=>[]}.tsort raises
	// TSort::Cyclic "topological sort failed: [2, 3]".
	g := newGraph().add(1, 2).add(2, 3, 4).add(3, 2).add(4)
	_, err := TSort(g.nodes, g.children)
	if err == nil {
		t.Fatal("expected Cyclic error")
	}
	var c *Cyclic
	if !errors.As(err, &c) {
		t.Fatalf("expected *Cyclic, got %T", err)
	}
	if want := "topological sort failed: [2, 3]"; c.Error() != want {
		t.Errorf("Cyclic.Error() = %q, want %q", c.Error(), want)
	}
	if want := []any{2, 3}; !eq(c.Component, want) {
		t.Errorf("Cyclic.Component = %v, want %v", c.Component, want)
	}
}

func TestTSortCyclicSymbols(t *testing.T) {
	// MRI: {:a=>[:b], :b=>[:c], :c=>[:a]} raises "...: [:a, :b, :c]".
	g := newGraph().
		add(Symbol("a"), Symbol("b")).
		add(Symbol("b"), Symbol("c")).
		add(Symbol("c"), Symbol("a"))
	_, err := TSort(g.nodes, g.children)
	if err == nil {
		t.Fatal("expected Cyclic error")
	}
	if want := "topological sort failed: [:a, :b, :c]"; err.Error() != want {
		t.Errorf("Cyclic.Error() = %q, want %q", err.Error(), want)
	}
}

func TestStronglyConnectedComponents(t *testing.T) {
	// MRI: {1=>[2,3], 2=>[3], 3=>[], 4=>[]}.scc #=> [[3], [2], [1], [4]]
	g := newGraph().add(1, 2, 3).add(2, 3).add(3).add(4)
	got := StronglyConnectedComponents(g.nodes, g.children)
	want := [][]any{{3}, {2}, {1}, {4}}
	if !eq(got, want) {
		t.Errorf("SCC = %v, want %v", got, want)
	}
}

func TestStronglyConnectedComponentsCycle(t *testing.T) {
	// MRI: {1=>[2], 2=>[3,4], 3=>[2], 4=>[]}.scc #=> [[4], [2, 3], [1]]
	g := newGraph().add(1, 2).add(2, 3, 4).add(3, 2).add(4)
	got := StronglyConnectedComponents(g.nodes, g.children)
	want := [][]any{{4}, {2, 3}, {1}}
	if !eq(got, want) {
		t.Errorf("SCC = %v, want %v", got, want)
	}
}

func TestStronglyConnectedComponentsEmpty(t *testing.T) {
	g := newGraph()
	got := StronglyConnectedComponents(g.nodes, g.children)
	if want := [][]any{}; !eq(got, want) {
		t.Errorf("SCC(empty) = %v, want %v", got, want)
	}
}

func TestStronglyConnectedComponentsDocGraph(t *testing.T) {
	// MRI: {1=>[2,3], 2=>[4], 3=>[2,4], 4=>[]}.scc #=> [[4], [2], [3], [1]]
	g := newGraph().add(1, 2, 3).add(2, 4).add(3, 2, 4).add(4)
	got := StronglyConnectedComponents(g.nodes, g.children)
	want := [][]any{{4}, {2}, {3}, {1}}
	if !eq(got, want) {
		t.Errorf("SCC = %v, want %v", got, want)
	}
}

func TestEachStronglyConnectedComponent(t *testing.T) {
	g := newGraph().add(1, 2).add(2, 3, 4).add(3, 2).add(4)
	var got [][]any
	EachStronglyConnectedComponent(g.nodes, g.children, func(c []any) {
		got = append(got, c)
	})
	want := [][]any{{4}, {2, 3}, {1}}
	if !eq(got, want) {
		t.Errorf("each SCC = %v, want %v", got, want)
	}
}

func TestEachStronglyConnectedComponentFrom(t *testing.T) {
	// MRI: each_strongly_connected_component_from(2) over
	// {1=>[2,3], 2=>[4], 3=>[2,4], 4=>[]} #=> [4], [2]
	g := newGraph().add(1, 2, 3).add(2, 4).add(3, 2, 4).add(4)
	var got [][]any
	EachStronglyConnectedComponentFrom(2, g.children, func(c []any) {
		got = append(got, c)
	})
	want := [][]any{{4}, {2}}
	if !eq(got, want) {
		t.Errorf("from(2) = %v, want %v", got, want)
	}
}

func TestEachStronglyConnectedComponentFromCycle(t *testing.T) {
	// MRI: each_strongly_connected_component_from(2) over
	// {1=>[2], 2=>[3,4], 3=>[2], 4=>[]} #=> [4], [2, 3]
	g := newGraph().add(1, 2).add(2, 3, 4).add(3, 2).add(4)
	var got [][]any
	EachStronglyConnectedComponentFrom(2, g.children, func(c []any) {
		got = append(got, c)
	})
	want := [][]any{{4}, {2, 3}}
	if !eq(got, want) {
		t.Errorf("from(2) cyclic = %v, want %v", got, want)
	}
}

// TestOptionsIdentity exercises a host-supplied Identity: distinct boxed nodes that
// are equal only through their key. Two *box values with the same id must be the
// same graph node.
func TestOptionsIdentity(t *testing.T) {
	type box struct{ id int }
	b1, b2, b3 := &box{1}, &box{2}, &box{3}
	// b1 has the same identity-key as b1dup; the graph references both pointers but
	// they must collapse to one node.
	b1dup := &box{1}
	g := newGraph().add(b1, b2).add(b2, b3).add(b3, b1dup).add(b1)
	opts := &Options{Identity: func(n any) any { return n.(*box).id }}
	scc := StronglyConnectedComponentsWith(g.nodes, g.children, opts)
	// b1,b2,b3 form one cycle (b1->b2->b3->b1dup==b1). One component of size 3.
	if len(scc) != 1 || len(scc[0]) != 3 {
		t.Fatalf("identity SCC = %v, want one size-3 component", scc)
	}
	_, err := TSortWith(g.nodes, g.children, opts)
	if err == nil {
		t.Fatal("expected Cyclic with identity graph")
	}
}

// TestOptionsInspect exercises a host-supplied Inspect used in the Cyclic message.
func TestOptionsInspect(t *testing.T) {
	g := newGraph().add("a", "b").add("b", "a")
	opts := &Options{Inspect: func(n any) string { return "<" + n.(string) + ">" }}
	_, err := TSortWith(g.nodes, g.children, opts)
	if err == nil {
		t.Fatal("expected Cyclic")
	}
	if want := "topological sort failed: [<a>, <b>]"; err.Error() != want {
		t.Errorf("custom inspect msg = %q, want %q", err.Error(), want)
	}
}

func TestEachStronglyConnectedComponentFromWith(t *testing.T) {
	type box struct{ id int }
	b1, b2 := &box{1}, &box{2}
	g := newGraph().add(b1, b2).add(b2, b1)
	opts := &Options{Identity: func(n any) any { return n.(*box).id }}
	var got [][]any
	EachStronglyConnectedComponentFromWith(b1, g.children, opts, func(c []any) {
		got = append(got, c)
	})
	if len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("from-with = %v, want one size-2 component", got)
	}
}

// TestRubyInspectCoverage drives rubyInspect across every value kind it renders, so
// the Cyclic-message formatter is exercised independently of any actual cycle.
func TestRubyInspectCoverage(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{nil, "nil"},
		{true, "true"},
		{false, "false"},
		{"hi", `"hi"`},
		{"q\"\\\n\t\r", `"q\"\\\n\t\r"`},
		{Symbol("sym"), ":sym"},
		{42, "42"},
		{int64(-7), "-7"},
		{int8(1), "1"},
		{uint(9), "9"},
		{3.14, "3.14"},
		{float32(2.0), "2.0"},
		{2.0, "2.0"},
		{[]any{1, Symbol("a"), "s"}, `[1, :a, "s"]`},
		{stringer{}, "STR"},
		{struct{ X int }{1}, "{1}"},
	}
	for _, c := range cases {
		if got := rubyInspect(c.v); got != c.want {
			t.Errorf("rubyInspect(%#v) = %q, want %q", c.v, got, c.want)
		}
	}
}

type stringer struct{}

func (stringer) String() string { return "STR" }

// TestCyclicShortCircuit ensures that once a cycle is found, later single-node
// components do not append to the result (the cyclic != nil guard).
func TestCyclicShortCircuit(t *testing.T) {
	// Node 5 (a lone DAG node) is enumerated after the 2<->3 cycle; TSort must
	// still return the Cyclic for the first offending component and nil result.
	g := newGraph().add(1, 2).add(2, 3).add(3, 2).add(4).add(5)
	res, err := TSort(g.nodes, g.children)
	if err == nil {
		t.Fatal("expected Cyclic")
	}
	if res != nil {
		t.Errorf("result must be nil on cycle, got %v", res)
	}
	var c *Cyclic
	if !errors.As(err, &c) {
		t.Fatalf("want *Cyclic, got %T", err)
	}
}
