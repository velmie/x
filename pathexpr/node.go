package pathexpr

type node interface{ eval(any) (any, error) }
type literalNode struct {
	value any
}
type pathNode struct {
	steps []step
	text  string
}
type objectNode struct {
	fields []field
}
type field struct {
	key   string
	value node
	pos   Position
}
type step struct {
	key     string
	index   uint64
	indexed bool
	pos     Position
	prefix  string
}
