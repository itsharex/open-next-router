package dsllang

import (
	"sort"
	"strings"
)

// CurrentBlock returns the innermost DSL block at pos.
func CurrentBlock(text string, pos Position) string {
	stack := CurrentBlockStack(text, pos)
	if len(stack) == 0 {
		return "top"
	}
	return stack[len(stack)-1]
}

// CurrentBlockStack returns the DSL block stack at pos.
func CurrentBlockStack(text string, pos Position) []string {
	toks := lex(text)
	stack := make([]string, 0, 8)
	pending := ""
	lockedPending := false

	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if tokenAfterPosition(tok, pos) {
			break
		}
		block := "top"
		if len(stack) > 0 {
			block = stack[len(stack)-1]
		}
		switch tok.kind {
		case tokIdent:
			if isStatementStart(toks, i) && blockAllowsChildBlock(block, tok.text) {
				pending = tok.text
				lockedPending = blockDirectiveNeedsHeader(block, tok.text)
			}
		case tokLBrace:
			name := pending
			if name == "" {
				name = "unknown"
			}
			stack = append(stack, name)
			pending = ""
			lockedPending = false
		case tokRBrace:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			pending = ""
			lockedPending = false
		case tokSemicolon:
			if !lockedPending {
				pending = ""
			}
		}
	}
	return stack
}

// CollectNamedModeBlocks returns named top-level mode block names of blockName.
func CollectNamedModeBlocks(text, blockName string) []string {
	toks := lex(text)
	stack := make([]string, 0, 8)
	pending := ""
	lockedPending := false
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)

	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		block := "top"
		if len(stack) > 0 {
			block = stack[len(stack)-1]
		}
		switch tok.kind {
		case tokIdent:
			if isStatementStart(toks, i) && block == "top" && tok.text == blockName {
				if name, ok := nextModeToken(toks, i+1); ok {
					v := normalizeModeToken(name)
					if v != "" {
						if _, exists := seen[v]; !exists {
							seen[v] = struct{}{}
							out = append(out, v)
						}
					}
				}
			}
			if isStatementStart(toks, i) && blockAllowsChildBlock(block, tok.text) {
				pending = tok.text
				lockedPending = blockDirectiveNeedsHeader(block, tok.text)
			}
		case tokLBrace:
			name := pending
			if name == "" {
				name = "unknown"
			}
			stack = append(stack, name)
			pending = ""
			lockedPending = false
		case tokRBrace:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			pending = ""
			lockedPending = false
		case tokSemicolon:
			if !lockedPending {
				pending = ""
			}
		}
	}

	sort.Strings(out)
	return out
}

func tokenAfterPosition(tok token, pos Position) bool {
	if tok.line > pos.Line {
		return true
	}
	if tok.line < pos.Line {
		return false
	}
	return tok.col > pos.Character
}

// BlockAllowsChildBlock reports whether child is a block directive in parent.
func BlockAllowsChildBlock(parent, child string) bool {
	return blockAllowsChildBlock(parent, child)
}

// BlockDirectiveNeedsHeader reports whether a block directive consumes a header
// before its opening brace.
func BlockDirectiveNeedsHeader(parent, name string) bool {
	return blockDirectiveNeedsHeader(parent, name)
}

// DedupeSortedStrings trims, deduplicates, and sorts string values.
func DedupeSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
