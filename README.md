<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-tsort/brand/main/social/go-ruby-tsort-tsort.png" alt="go-ruby-tsort/tsort" width="720"></p>

# tsort — go-ruby-tsort

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-tsort.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of Ruby's [`TSort`](https://docs.ruby-lang.org/en/master/TSort.html)
standard library** — topological sorting and strongly connected components over an
arbitrary directed graph, using **Tarjan's algorithm exactly as MRI 4.0.5's
`tsort.rb` does**. Component grouping, each component's internal ordering, and the
reverse-topological order in which components are emitted all match MRI
byte-for-byte — including the quirks (a self-loop is a single-node SCC and so does
**not** raise; the `TSort::Cyclic` message is `topological sort failed: [...]`).

It is the topological-sort backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but is a
**standalone, reusable** module with no dependency on the Ruby runtime — a sibling
of [go-ruby-regexp](https://github.com/go-ruby-regexp/regexp) (the Onigmo engine),
[go-ruby-erb](https://github.com/go-ruby-erb/erb) (the ERB compiler), and
[go-ruby-yaml](https://github.com/go-ruby-yaml/yaml) (the Psych emitter/loader).

> **What it is.** `TSort` interprets *any* object as a graph through two
> callbacks: one that yields every node, one that yields a node's children. This
> package models that functionally — you supply the two iterators and get back the
> sorted nodes (or the SCCs). Nodes are arbitrary `any` values; their graph
> identity (Ruby's `eql?` / `hash`) is host-supplied via `Options.Identity`, so a
> host such as rbgo can plug in boxed Ruby objects, while the library's own callers
> use plain comparable Go values.

## Features

Faithful port of `tsort.rb`, validated against the `ruby` binary on every
supported platform:

- **`TSort`** — nodes sorted children-first (`a` depends-on `b` → `b` precedes
  `a`); raises `*Cyclic` on a real cycle (an SCC of size ≥ 2) with MRI's exact
  message; a self-loop, being a size-1 SCC, returns normally.
- **`StronglyConnectedComponents`** — Tarjan SCCs in reverse topological order;
  each component's internal order is its DFS-discovery order, matching MRI.
- **`EachStronglyConnectedComponent`** / **`…From`** — the iterator forms,
  including the `…From(start, …)` variant that walks only the subgraph reachable
  from a start node and never enumerates the full node set.
- **Host identity & inspect** — `Options.Identity` decides node equality;
  `Options.Inspect` renders nodes for the `Cyclic` message (a built-in Ruby-style
  `#inspect` covers the numeric tower, strings, `Symbol`, booleans, `nil`, and
  arrays for the library's own callers).

CGO-free, dependency-free, **100% test coverage**, `gofmt` + `go vet` clean, and
green across the six 64-bit Go targets (amd64, arm64, riscv64, loong64, ppc64le,
s390x) and three operating systems (Linux, macOS, Windows).

## Install

```sh
go get github.com/go-ruby-tsort/tsort
```

## Usage

```go
package main

import (
	"fmt"

	"github.com/go-ruby-tsort/tsort"
)

func main() {
	// {1=>[2,3], 2=>[3], 3=>[], 4=>[]} — model it as two iterators.
	graph := map[int][]int{1: {2, 3}, 2: {3}, 3: {}, 4: {}}
	order := []int{1, 2, 3, 4} // enumeration order (a Ruby Hash is ordered)

	nodes := func(yield func(any)) {
		for _, n := range order {
			yield(n)
		}
	}
	children := func(node any, yield func(any)) {
		for _, c := range graph[node.(int)] {
			yield(c)
		}
	}

	sorted, err := tsort.TSort(nodes, children)
	fmt.Println(sorted, err) // [3 2 1 4] <nil>

	scc := tsort.StronglyConnectedComponents(nodes, children)
	fmt.Println(scc) // [[3] [2] [1] [4]]
}
```

A cycle returns a `*tsort.Cyclic` whose message matches MRI:

```go
// {1=>[2], 2=>[3,4], 3=>[2], 4=>[]} → tsort raises TSort::Cyclic
_, err := tsort.TSort(nodes, children)
// err.Error() == "topological sort failed: [2, 3]"
```

## API

```go
// NodesFunc yields every node; ChildrenFunc yields a node's children.
type NodesFunc    = func(yield func(any))
type ChildrenFunc = func(node any, yield func(any))

// TSort returns nodes sorted children-first, or a *Cyclic on a real cycle.
func TSort(nodes NodesFunc, children ChildrenFunc) ([]any, error)

// StronglyConnectedComponents returns SCCs in reverse topological order.
func StronglyConnectedComponents(nodes NodesFunc, children ChildrenFunc) [][]any

// Iterator forms.
func EachStronglyConnectedComponent(nodes NodesFunc, children ChildrenFunc, yield func(component []any))
func EachStronglyConnectedComponentFrom(start any, children ChildrenFunc, yield func(component []any))

// *With variants take Options for host-supplied node identity / inspect.
func TSortWith(nodes NodesFunc, children ChildrenFunc, opts *Options) ([]any, error)
func StronglyConnectedComponentsWith(nodes NodesFunc, children ChildrenFunc, opts *Options) [][]any
func EachStronglyConnectedComponentWith(nodes NodesFunc, children ChildrenFunc, opts *Options, yield func([]any))
func EachStronglyConnectedComponentFromWith(start any, children ChildrenFunc, opts *Options, yield func([]any))

// Options mirrors Ruby's eql?/hash (Identity) and #inspect (Inspect).
type Options struct {
	Identity func(node any) any    // graph-equality key; nil ⇒ node must be comparable
	Inspect  func(node any) string // for the Cyclic message; nil ⇒ built-in Ruby-style
}

// Cyclic is the equivalent of Ruby's TSort::Cyclic.
type Cyclic struct{ Component []any /* … */ }
func (c *Cyclic) Error() string // "topological sort failed: [...]"

// Symbol is a node value that inspects as a Ruby symbol (":name").
type Symbol string
```

## Tests & coverage

The suite pairs deterministic, ruby-free tests (which alone hold coverage at
100%, so the qemu cross-arch and Windows lanes pass the gate) with a **differential
MRI oracle**: a corpus of fixed and deterministically-random digraphs is sorted
both here and by the system `ruby` (`TSort.tsort`,
`TSort.strongly_connected_components`, `each_strongly_connected_component_from`),
and the results — including `TSort::Cyclic` messages — must match byte-for-byte.
The oracle scripts `$stdout.binmode` so Windows text-mode never pollutes the
bytes, gate on `RUBY_VERSION >= "4.0"`, and skip themselves where `ruby` is absent.

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-tsort/tsort authors.
