// Package indenttree reconstructs a describe/context/it (or describe/it) tree
// from indented console lines, using a simple monotonic-indent stack — no
// hardcoded "N spaces per level" assumption. Shared by the rspecdoc and
// mochaspec log-step extractors, which both narrate nested test groups this
// way.
package indenttree

// Line is one line of captured, already ANSI-stripped console output, with
// its leading-indent width and trimmed text.
type Line struct {
	Indent int
	Text   string
	Raw    string
}

// Node is one line's place in the reconstructed tree.
type Node struct {
	Line     Line
	Parent   *Node
	Children []*Node
}

// Build reconstructs a tree from lines using a monotonic-indent stack:
// a line nests under the most recent line with a strictly smaller indent.
func Build(lines []Line) []*Node {
	var roots []*Node
	var stack []*Node
	for _, l := range lines {
		n := &Node{Line: l}
		for len(stack) > 0 && stack[len(stack)-1].Line.Indent >= l.Indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, n)
		} else {
			parent := stack[len(stack)-1]
			n.Parent = parent
			parent.Children = append(parent.Children, n)
		}
		stack = append(stack, n)
	}
	return roots
}

// Leaves returns every node with no children, in tree order — the
// examples/tests a describe/context/it (or describe/it) tree distinguishes
// from group headers, for free, via the tree shape itself.
func Leaves(roots []*Node) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(n *Node) {
		if len(n.Children) == 0 {
			out = append(out, n)
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return out
}

// Ancestors returns node's ancestor chain, root first, ending with node
// itself.
func Ancestors(node *Node) []*Node {
	var chain []*Node
	for n := node; n != nil; n = n.Parent {
		chain = append(chain, n)
	}
	// Reverse in place: chain was built leaf-to-root.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}
