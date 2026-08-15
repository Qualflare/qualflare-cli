package indenttree

import (
	"reflect"
	"testing"
)

func TestBuildSingleRootNoChildren(t *testing.T) {
	roots := Build([]Line{{Indent: 0, Text: "a"}})
	if len(roots) != 1 || roots[0].Line.Text != "a" || len(roots[0].Children) != 0 {
		t.Fatalf("unexpected tree: %+v", roots)
	}
}

func TestBuildNestsByIncreasingIndent(t *testing.T) {
	roots := Build([]Line{
		{Indent: 0, Text: "describe"},
		{Indent: 2, Text: "it"},
	})
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if len(roots[0].Children) != 1 || roots[0].Children[0].Line.Text != "it" {
		t.Fatalf("expected 'it' nested under 'describe', got %+v", roots[0])
	}
	if roots[0].Children[0].Parent != roots[0] {
		t.Error("child's Parent pointer must point back to its parent")
	}
}

func TestBuildSiblingsAtSameIndent(t *testing.T) {
	roots := Build([]Line{
		{Indent: 0, Text: "describe"},
		{Indent: 2, Text: "it a"},
		{Indent: 2, Text: "it b"},
	})
	if len(roots[0].Children) != 2 {
		t.Fatalf("expected 2 siblings, got %d: %+v", len(roots[0].Children), roots[0].Children)
	}
}

func TestBuildDedentPopsBackToAncestor(t *testing.T) {
	roots := Build([]Line{
		{Indent: 0, Text: "outer"},
		{Indent: 2, Text: "inner"},
		{Indent: 4, Text: "leaf"},
		{Indent: 2, Text: "sibling of inner"},
	})
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if len(roots[0].Children) != 2 {
		t.Fatalf("expected 2 children of outer (inner, sibling of inner), got %d: %+v", len(roots[0].Children), roots[0].Children)
	}
	if roots[0].Children[1].Line.Text != "sibling of inner" {
		t.Errorf("expected dedent to pop back to outer's level, got %+v", roots[0].Children[1])
	}
}

func TestBuildMultipleRoots(t *testing.T) {
	roots := Build([]Line{
		{Indent: 0, Text: "first"},
		{Indent: 0, Text: "second"},
	})
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %d", len(roots))
	}
}

func TestBuildIndentWidthIsRelativeNotFixed(t *testing.T) {
	// Varying indent widths (not a fixed "2 spaces per level") must still nest
	// correctly — indenttree only cares that a line's indent is greater than
	// its would-be parent's, not by how much.
	roots := Build([]Line{
		{Indent: 0, Text: "a"},
		{Indent: 3, Text: "b"},
		{Indent: 7, Text: "c"},
	})
	if len(roots) != 1 || len(roots[0].Children) != 1 || len(roots[0].Children[0].Children) != 1 {
		t.Fatalf("unexpected tree shape: %+v", roots)
	}
}

func TestLeavesReturnsOnlyChildlessNodes(t *testing.T) {
	roots := Build([]Line{
		{Indent: 0, Text: "outer"},
		{Indent: 2, Text: "inner"},
		{Indent: 4, Text: "leaf1"},
		{Indent: 2, Text: "leaf2"},
	})
	leaves := Leaves(roots)
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leaves, got %d: %+v", len(leaves), leaves)
	}
	texts := []string{leaves[0].Line.Text, leaves[1].Line.Text}
	if !reflect.DeepEqual(texts, []string{"leaf1", "leaf2"}) {
		t.Errorf("expected leaves in tree order [leaf1, leaf2], got %v", texts)
	}
}

func TestLeavesSingleRootWithNoChildrenIsItsOwnLeaf(t *testing.T) {
	roots := Build([]Line{{Indent: 0, Text: "solo"}})
	leaves := Leaves(roots)
	if len(leaves) != 1 || leaves[0].Line.Text != "solo" {
		t.Fatalf("expected the single root to be its own leaf, got %+v", leaves)
	}
}

func TestAncestorsReturnsRootFirstEndingWithSelf(t *testing.T) {
	roots := Build([]Line{
		{Indent: 0, Text: "outer"},
		{Indent: 2, Text: "inner"},
		{Indent: 4, Text: "leaf"},
	})
	leaf := Leaves(roots)[0]
	chain := Ancestors(leaf)
	texts := make([]string, len(chain))
	for i, n := range chain {
		texts[i] = n.Line.Text
	}
	if !reflect.DeepEqual(texts, []string{"outer", "inner", "leaf"}) {
		t.Errorf("expected root-first ancestor chain ending with self, got %v", texts)
	}
}

func TestAncestorsOfARootIsJustItself(t *testing.T) {
	roots := Build([]Line{{Indent: 0, Text: "solo"}})
	chain := Ancestors(roots[0])
	if len(chain) != 1 || chain[0].Line.Text != "solo" {
		t.Fatalf("expected a single-element chain, got %+v", chain)
	}
}
