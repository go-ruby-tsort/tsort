// Copyright (c) the go-ruby-tsort/tsort authors
//
// SPDX-License-Identifier: BSD-3-Clause

package tsort

import (
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"testing"
)

// rubyBin locates a usable `ruby` once. The oracle tests skip themselves when it is
// absent (the qemu cross-arch lanes and the Windows lane), so the deterministic
// suite alone drives the 100% gate there. The oracle also requires Ruby >= 4.0.
func rubyBin(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping MRI oracle")
	}
	ver := strings.TrimSpace(rubyEval(t, path,
		"$stdout.binmode\n$stdin.binmode if $stdin.respond_to?(:binmode)\nprint RUBY_VERSION"))
	if ver < "4.0" {
		t.Skipf("ruby %s < 4.0; skipping MRI oracle", ver)
	}
	return path
}

// rubyEval runs a Ruby script and returns its stdout. The script $stdout.binmode's
// itself (and binmodes stdin) so Windows text-mode does not pollute the bytes.
func rubyEval(t *testing.T, bin, script string) string {
	t.Helper()
	cmd := exec.Command(bin, "-rtsort", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ruby error: %v\nscript:\n%s\noutput:\n%s", err, script, out)
	}
	return string(out)
}

// preamble builds the shared Ruby harness: it reads a graph (an ordered list of
// [node, [children...]] pairs) from a literal, exposes each_node/each_child
// lambdas, and binmodes the streams for the Windows lane.
const oraclePreamble = "$stdout.binmode\n$stdin.binmode if $stdin.respond_to?(:binmode)\n" +
	"G = %s\n" +
	"H = {}\n" +
	"G.each {|n, cs| H[n] = cs }\n" +
	"EN = lambda {|&b| H.each_key(&b) }\n" +
	"EC = lambda {|n, &b| H[n].each(&b) }\n"

// rubyGraphLiteral renders an ordered integer graph as a Ruby array-of-pairs literal
// preserving order, e.g. [[1,[2,3]],[2,[3]],[3,[]]].
func rubyGraphLiteral(order []int, adj map[int][]int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, n := range order {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "[%d,[", n)
		for j, c := range adj[n] {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%d", c)
		}
		b.WriteString("]]")
	}
	b.WriteByte(']')
	return b.String()
}

// goGraph adapts an ordered integer graph to the tsort callbacks.
func goGraph(order []int, adj map[int][]int) (NodesFunc, ChildrenFunc) {
	nodes := func(yield func(any)) {
		for _, n := range order {
			yield(n)
		}
	}
	children := func(node any, yield func(any)) {
		for _, c := range adj[node.(int)] {
			yield(c)
		}
	}
	return nodes, children
}

// TestOracleSCCMatchesMRI checks strongly_connected_components against MRI over a
// battery of fixed and random graphs: the full nested array structure must match
// MRI's Tarjan output byte-for-byte (grouping + per-component discovery order +
// reverse-topo component order).
func TestOracleSCCMatchesMRI(t *testing.T) {
	bin := rubyBin(t)
	for i, tc := range oracleGraphs(t) {
		order, adj := tc.order, tc.adj
		lit := rubyGraphLiteral(order, adj)
		script := fmt.Sprintf(oraclePreamble, lit) +
			"print TSort.strongly_connected_components(EN, EC).inspect"
		want := rubyEval(t, bin, script)

		nodes, children := goGraph(order, adj)
		got := goSCCInspect(StronglyConnectedComponents(nodes, children))
		if got != want {
			t.Errorf("graph[%d] %s\n SCC = %s\n MRI = %s", i, lit, got, want)
		}
	}
}

// TestOracleTSortMatchesMRI checks tsort against MRI: either both raise Cyclic with
// the identical message, or both return the identical ordering.
func TestOracleTSortMatchesMRI(t *testing.T) {
	bin := rubyBin(t)
	for i, tc := range oracleGraphs(t) {
		order, adj := tc.order, tc.adj
		lit := rubyGraphLiteral(order, adj)
		// MRI: print the sorted array, or "CYCLIC:" + the message.
		script := fmt.Sprintf(oraclePreamble, lit) +
			"begin\n  print TSort.tsort(EN, EC).inspect\n" +
			"rescue TSort::Cyclic => e\n  print 'CYCLIC:' + e.message\nend"
		want := rubyEval(t, bin, script)

		nodes, children := goGraph(order, adj)
		res, err := TSort(nodes, children)
		var got string
		if err != nil {
			got = "CYCLIC:" + err.Error()
		} else {
			got = goArrInspect(res)
		}
		if got != want {
			t.Errorf("graph[%d] %s\n tsort = %s\n MRI   = %s", i, lit, got, want)
		}
	}
}

// TestOracleFromMatchesMRI checks each_strongly_connected_component_from against MRI
// starting from every node of each graph.
func TestOracleFromMatchesMRI(t *testing.T) {
	bin := rubyBin(t)
	for i, tc := range oracleGraphs(t) {
		order, adj := tc.order, tc.adj
		if len(order) == 0 {
			continue
		}
		lit := rubyGraphLiteral(order, adj)
		for _, start := range order {
			script := fmt.Sprintf(oraclePreamble, lit) +
				fmt.Sprintf("r = []\nTSort.each_strongly_connected_component_from(%d, EC) {|c| r << c }\nprint r.inspect", start)
			want := rubyEval(t, bin, script)

			_, children := goGraph(order, adj)
			var got [][]any
			EachStronglyConnectedComponentFrom(start, children, func(c []any) { got = append(got, c) })
			if g := goSCCInspect(got); g != want {
				t.Errorf("graph[%d] %s from %d\n from = %s\n MRI  = %s", i, lit, start, g, want)
			}
		}
	}
}

// goArrInspect renders []any of ints the way Ruby's Array#inspect would.
func goArrInspect(a []any) string {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = fmt.Sprintf("%d", v.(int))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// goSCCInspect renders [][]any of ints the way Ruby's Array#inspect would.
func goSCCInspect(scc [][]any) string {
	parts := make([]string, len(scc))
	for i, c := range scc {
		parts[i] = goArrInspect(c)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

type oracleGraph struct {
	order []int
	adj   map[int][]int
}

// oracleGraphs returns the corpus: hand-picked structures (DAGs, cycles, self-loops,
// disconnected, the MRI doc examples) plus a deterministic batch of random digraphs.
func oracleGraphs(t *testing.T) []oracleGraph {
	t.Helper()
	mk := func(pairs ...[]int) oracleGraph {
		g := oracleGraph{adj: map[int][]int{}}
		for _, p := range pairs {
			n := p[0]
			if _, ok := g.adj[n]; !ok {
				g.order = append(g.order, n)
			}
			g.adj[n] = append(g.adj[n], p[1:]...)
		}
		return g
	}
	fixed := []oracleGraph{
		{order: []int{}, adj: map[int][]int{}}, // empty
		mk([]int{1}),                           // single isolated
		mk([]int{1, 1}),                        // self-loop
		mk([]int{1, 2, 3}, []int{2, 3}, []int{3}, []int{4}),       // doc DAG #1
		mk([]int{1, 2, 3}, []int{2, 4}, []int{3, 2, 4}, []int{4}), // doc graph
		mk([]int{1, 2}, []int{2, 3, 4}, []int{3, 2}, []int{4}),    // doc cycle
		mk([]int{1, 2}, []int{2, 3}, []int{3, 1}),                 // 3-cycle
		mk([]int{1, 2}, []int{2, 1}, []int{3, 4}, []int{4, 3}),    // two 2-cycles
		mk([]int{1, 2, 3, 4}, []int{2}, []int{3}, []int{4}),       // fan-out
		mk([]int{1}, []int{2}, []int{3}, []int{4}),                // all isolated
		mk([]int{1, 2}, []int{2, 3}, []int{3, 4}, []int{4, 2}),    // tail into cycle
	}
	// Deterministic random batch.
	rng := rand.New(rand.NewSource(20260629))
	for k := 0; k < 60; k++ {
		n := 1 + rng.Intn(7)
		g := oracleGraph{adj: map[int][]int{}}
		for i := 1; i <= n; i++ {
			g.order = append(g.order, i)
			g.adj[i] = nil
		}
		for _, from := range g.order {
			deg := rng.Intn(n + 1)
			for d := 0; d < deg; d++ {
				g.adj[from] = append(g.adj[from], 1+rng.Intn(n))
			}
		}
		fixed = append(fixed, g)
	}
	return fixed
}
