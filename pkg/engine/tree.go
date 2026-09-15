package engine

import (
	"strings"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

// childrenKey is where a node's children go. It is the same name at
// every depth, so a consumer walks a tree with one rule rather than one
// per format.
const childrenKey = "children"

// parseTree handles type: tree.
//
// It is the one parser whose result has a shape the definition does not
// fully state. What the definition says is what a node is: the fields
// read from its line, plus a children array. What the input says is how
// deep the nodes go, and only that. A report where a device has
// capabilities and a capability has flags (lspci -vv), or where a
// configuration has an interface and an interface has an endpoint
// (lsusb -v), cannot be described any other way: the depth is a property
// of the machine being described, not of the format.
//
// Every line is a node. That keeps the rule to one sentence and makes
// the depth the only thing the input decides; grouping runs of lines
// into one node would make a node's identity depend on its siblings.
func (r *run) parseTree(p *definition.Parse, lines []line) (any, error) {
	nodes, _, err := r.treeNodes(p, lines, 0, 0)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// treeNodes reads the nodes at depth from lines[i:] and returns them
// together with the index of the first line that belongs to a shallower
// level.
func (r *run) treeNodes(p *definition.Parse, lines []line, i, depth int) ([]any, int, error) {
	out := []any{}
	for i < len(lines) {
		d, text, err := r.treeDepth(p, lines[i])
		if err != nil {
			return nil, 0, err
		}
		if d < depth {
			return out, i, nil
		}
		if d > depth {
			// The caller reads a node's children immediately after it, so
			// a line deeper than expected here has skipped a level: there
			// is no node for it to hang from.
			return nil, 0, r.errorf(lines[i].num, "", "indented %d levels below a line at level %d, so it has no parent", d-depth, depth)
		}
		// A definition that says what a top-level line looks like is
		// saying that a line at depth zero of any other shape is not
		// part of the report, however well a node pattern reads it.
		if re := p.CompiledRoot(); depth == 0 && re != nil && !re.MatchString(text) {
			return nil, 0, r.errorf(lines[i].num, "", "line at the top level does not match root %s: %q", shortPattern(p.Root), truncate(text, 80))
		}
		obj, err := r.treeNode(p, line{text: text, num: lines[i].num})
		if err != nil {
			return nil, 0, err
		}
		i++
		children, next, err := r.treeNodes(p, lines, i, depth+1)
		if err != nil {
			return nil, 0, err
		}
		obj.Set(childrenKey, children)
		i = next
		out = append(out, obj)
	}
	return out, i, nil
}

// treeDepth counts how many levels of indentation open the line and
// returns the rest of it. Where a definition states several forms one
// level may take, the first that fits at each step is the one taken.
//
// A form that ends in a character other than a blank is a branch drawn to
// the node ("|-", "└─"), and it ends the indentation: nothing deeper is
// drawn after a branch on the same line, so what follows it is the node's
// text, blanks included. systemd-cgls pads a process id to the width of
// its siblings after the branch, and those spaces are not another level.
func (r *run) treeDepth(p *definition.Parse, l line) (int, string, error) {
	text, depth := l.text, 0
	for {
		unit, ok := openingUnit(p.Indent, text)
		if !ok {
			break
		}
		text = text[len(unit):]
		depth++
		if depth > definition.MaxTreeDepth {
			return 0, "", r.errorf(l.num, "", "nested deeper than %d levels", definition.MaxTreeDepth)
		}
		if last := unit[len(unit)-1]; last != ' ' && last != '\t' {
			return depth, text, nil
		}
	}
	// Leading whitespace that is not a whole number of levels means the
	// indentation the definition states is not the one the text uses, and
	// rounding it down would put a node under the wrong parent.
	if rest := strings.TrimLeft(text, " \t"); len(rest) != len(text) {
		return 0, "", r.errorf(l.num, "", "indented by something other than a whole number of %v", []string(p.Indent))
	}
	return depth, text, nil
}

// openingUnit returns the first stated form of one level that opens text.
func openingUnit(indent definition.Indent, text string) (string, bool) {
	for _, unit := range indent {
		if unit != "" && strings.HasPrefix(text, unit) {
			return unit, true
		}
	}
	return "", false
}

// treeNode reads one line into the object for it.
func (r *run) treeNode(p *definition.Parse, l line) (*jsonutil.Object, error) {
	n := p.Node
	if n.Parse.Type == definition.TypeKV {
		v, err := r.parseKV(&n.Parse, n.Fields, []line{l})
		if err != nil {
			return nil, err
		}
		list, ok := v.([]any)
		if !ok || len(list) != 1 {
			return nil, r.errorf(l.num, "", "expected one key/value entry")
		}
		obj, ok := list[0].(*jsonutil.Object)
		if !ok {
			return nil, r.errorf(l.num, "", "expected one key/value entry")
		}
		return obj, nil
	}
	re, m, which := firstMatch(n.Parse.CompiledPatterns(), l.text)
	if m == nil {
		return nil, r.errorf(l.num, "", "line does not match %s: %q", describePatterns(&n.Parse), truncate(l.text, 80))
	}
	if err := r.checkWhole(l, m[0], m[1]); err != nil {
		return nil, err
	}
	return r.objectFromMatch(re, l.text, m, n.Parse.PatternValues(which), n.Fields, l.num)
}
