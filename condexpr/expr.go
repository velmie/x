// Package condexpr compiles boolean conditions over in-memory Go values.
package condexpr

import "slices"

// Expr is an immutable condition obtained through Compile.
type Expr struct {
	root  *node
	paths []string
}

// Info describes all paths in a condition, including short-circuited branches.
type Info struct {
	// Paths lists all canonical path occurrences in source order.
	Paths []string
}

// Compile validates the entire condition without accessing data.
func Compile(source string) (Expr, error) {
	p := parser{
		lex: lexer{
			s: source,
		},
	}
	p.next()
	n := p.or()
	if p.err == nil && p.t.kind != "" {
		p.fail("unexpected trailing input")
	}
	if p.err != nil {
		return Expr{}, p.err
	}
	return Expr{
		root:  n,
		paths: p.paths,
	}, nil
}

// Eval tests root without modifying it. Failure returns false and an error.
// Concurrent reuse requires that callers do not concurrently mutate root.
func (e Expr) Eval(root any) (bool, error) { return e.root.eval(root) }

// Inspect returns caller-owned information, retaining repeated path occurrences.
func (e Expr) Inspect() Info {
	return Info{
		Paths: slices.Clone(e.paths),
	}
}
