package pathexpr

// Info describes all syntax in an expression, including fields that may be omitted.
type Info struct {
	// IsObject reports whether the root constructs an object.
	IsObject bool
	// Paths lists all canonical path occurrences in source order.
	Paths []string
	// Keys lists all output-key occurrences in source order.
	Keys []string
	// Depth is the maximum number of nested object constructors.
	Depth int
}

// Inspect returns caller-owned information in source order. Paths and Keys
// retain occurrences, and Depth counts nested object constructors.
func (e Expr) Inspect() Info {
	info := Info{}
	_, info.IsObject = e.root.(objectNode)
	inspectNode(e.root, 0, &info)
	return info
}

func inspectNode(n node, depth int, info *Info) {
	switch n := n.(type) {
	case pathNode:
		info.Paths = append(info.Paths, n.text)
	case objectNode:
		depth++
		if depth > info.Depth {
			info.Depth = depth
		}
		for _, field := range n.fields {
			info.Keys = append(info.Keys, field.key)
			inspectNode(field.value, depth, info)
		}
	}
}
