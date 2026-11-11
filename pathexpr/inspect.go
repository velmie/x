package pathexpr

// Info describes all syntax in an expression, including fields that may be omitted.
type Info struct {
	IsObject bool
	Paths    []string
	Keys     []string
	Depth    int
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
