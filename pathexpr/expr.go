// Package pathexpr compiles expressions for selecting and constructing values.
package pathexpr

// Expr is an immutable expression obtained through Compile.
type Expr struct {
	root node
}

// Compile validates the entire expression without accessing data.
func Compile(source string) (Expr, error) {
	p, err := newParser(source)
	if err != nil {
		return Expr{}, err
	}
	n, err := p.expression()
	if err != nil {
		return Expr{}, err
	}
	p.space()
	if p.i != len(p.s) {
		return Expr{}, p.fail("unexpected trailing input")
	}
	return Expr{
		root: n,
	}, nil
}

// Eval constructs a value without modifying root. Selected containers may alias
// root, while constructed objects are fresh. Failure returns nil and an error.
func (e Expr) Eval(root any) (any, error) { return e.root.eval(root) }
