package glob

import (
	"fmt"
	"github.com/gobwas/glob/match"
)

func optimize(matcher match.Matcher) match.Matcher {
	switch m := matcher.(type) {

	case match.Any:
		if m.Separators == "" {
			return match.Super{}
		}

	case match.BTree:
		m.Left = optimize(m.Left)
		m.Right = optimize(m.Right)

		r, ok := m.Value.(match.Raw)
		if !ok {
			return m
		}

		leftNil := m.Left == nil
		rightNil := m.Right == nil

		if leftNil && rightNil {
			return match.Raw{r.Str}
		}

		_, leftSuper := m.Left.(match.Super)
		lp, leftPrefix := m.Left.(match.Prefix)

		_, rightSuper := m.Right.(match.Super)
		rs, rightSuffix := m.Right.(match.Suffix)

		if leftSuper && rightSuper {
			return match.Contains{r.Str, false}
		}

		if leftSuper && rightNil {
			return match.Suffix{r.Str}
		}

		if rightSuper && leftNil {
			return match.Prefix{r.Str}
		}

		if leftNil && rightSuffix {
			return match.Every{match.Matchers{match.Prefix{r.Str}, rs}}
		}

		if rightNil && leftPrefix {
			return match.Every{match.Matchers{lp, match.Suffix{r.Str}}}
		}

		return m
	}

	return matcher
}

func glueMatchers(matchers []match.Matcher) match.Matcher {
	switch len(matchers) {
	case 0:
		return nil
	case 1:
		return matchers[0]
	}

	var (
		hasAny    bool
		hasSuper  bool
		hasSingle bool
		min       int
		separator string
	)

	for i, matcher := range matchers {
		var sep string
		switch m := matcher.(type) {

		case match.Super:
			sep = ""
			hasSuper = true

		case match.Any:
			sep = m.Separators
			hasAny = true

		case match.Single:
			sep = m.Separators
			hasSingle = true
			min++

		case match.List:
			if !m.Not {
				return nil
			}
			sep = m.List
			hasSingle = true
			min++

		default:
			return nil
		}

		// initialize
		if i == 0 {
			separator = sep
		}

		if sep == separator {
			continue
		}

		return nil
	}

	if hasSuper && !hasAny && !hasSingle {
		return match.Super{}
	}

	if hasAny && !hasSuper && !hasSingle {
		return match.Any{separator}
	}

	if (hasAny || hasSuper) && min > 0 && separator == "" {
		return match.Min{min}
	}

	every := match.Every{}

	if min > 0 {
		every.Add(match.Min{min})

		if !hasAny && !hasSuper {
			every.Add(match.Max{min})
		}
	}

	if separator != "" {
		every.Add(match.Contains{separator, true})
	}

	return every
}

func convertMatchers(matchers []match.Matcher) (match.Matcher, error) {
	if m := glueMatchers(matchers); m != nil {
		return m, nil
	}

	var (
		val match.Primitive
		idx int
	)

	for i, matcher := range matchers {
		if p, ok := matcher.(match.Primitive); ok {
			idx = i
			val = p

			if _, ok := matcher.(match.Raw); ok {
				break
			}
		}
	}

	if val == nil {
		return nil, fmt.Errorf("could not convert matchers %s: need at least one primitive", match.Matchers(matchers))
	}

	left := matchers[:idx]
	var right []match.Matcher
	if len(matchers) > idx+1 {
		right = matchers[idx+1:]
	}

	tree := match.BTree{Value: val}

	if len(left) > 0 {
		l, err := convertMatchers(left)
		if err != nil {
			return nil, err
		}

		tree.Left = l
	}

	if len(right) > 0 {
		r, err := convertMatchers(right)
		if err != nil {
			return nil, err
		}

		tree.Right = r
	}

	return tree, nil
}

func do(node node, s string) (m match.Matcher, err error) {
	switch n := node.(type) {

	case *nodeAnyOf, *nodePattern:
		var matchers []match.Matcher
		for _, desc := range node.children() {
			m, err := do(desc, s)
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, optimize(m))
		}

		if _, ok := node.(*nodeAnyOf); ok {
			m = match.AnyOf{matchers}
		} else {
			m, err = convertMatchers(matchers)
			if err != nil {
				return nil, err
			}
		}

	case *nodeList:
		m = match.List{n.chars, n.not}

	case *nodeRange:
		m = match.Range{n.lo, n.hi, n.not}

	case *nodeAny:
		m = match.Any{s}

	case *nodeSuper:
		m = match.Super{}

	case *nodeSingle:
		m = match.Single{s}

	case *nodeText:
		m = match.Raw{n.text}

	default:
		return nil, fmt.Errorf("could not compile tree: unknown node type")
	}

	return optimize(m), nil
}

func compile(ast *nodePattern, s string) (Glob, error) {
	g, err := do(ast, s)
	if err != nil {
		return nil, err
	}

	return g, nil
}