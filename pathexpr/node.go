package pathexpr

type node interface{ eval(any) (any, error) }
type literalNode struct {
	value any
}
type pathNode struct {
	steps []step
	text  string
	pos   Position
}
type objectNode struct {
	fields []field
}
type field struct {
	key   string
	value node
	pos   Position
	omit  omission
}
type step struct {
	key     string
	index   uint64
	indexed bool
	pos     Position
	prefix  string
}

type omission uint8

const (
	omitNone omission = iota
	omitMissing
	omitEmpty
)
