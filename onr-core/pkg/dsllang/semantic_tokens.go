package dsllang

import (
	"strings"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslspec"
)

const (
	semanticTypeKeyword = iota
	semanticTypeString
	semanticTypeNumber
	semanticTypeComment
	semanticTypeOperator
	semanticTypeNamespace
	semanticTypeProperty
	semanticTypeEnumMember
)

var semanticTokenLegendTypes = []string{
	"keyword",
	"string",
	"number",
	"comment",
	"operator",
	"namespace",
	"property",
	"enumMember",
}

type semanticTokens struct {
	Data []uint32 `json:"data"`
}

type semanticLexKind int

const (
	semanticLexIdent semanticLexKind = iota
	semanticLexString
	semanticLexNumber
	semanticLexComment
	semanticLexOperator
	semanticLexLBrace
	semanticLexRBrace
	semanticLexSemicolon
	semanticLexOther
)

type semanticLexToken struct {
	kind   semanticLexKind
	text   string
	line   int
	col    int
	length int
}

type semanticSpan struct {
	line   int
	start  int
	length int
	typ    int
}

const (
	prevSigBOF = iota
	prevSigLBrace
	prevSigRBrace
	prevSigSemicolon
	prevSigOther
)

func semanticTokensFull(text string) semanticTokens {
	spans := classifySemanticSpans(text)
	return semanticTokens{Data: encodeSemanticSpans(spans)}
}

func classifySemanticSpans(text string) []semanticSpan {
	toks := lexSemantic(text)
	if len(toks) == 0 {
		return nil
	}

	spans := make([]semanticSpan, 0, len(toks))
	stack := make([]string, 0, 8)
	pending := ""
	lockedPending := false
	currentDirective := ""
	modePending := false
	prevSig := prevSigBOF

	for _, tok := range toks {
		switch tok.kind {
		case semanticLexComment:
			spans = append(spans, semanticSpan{line: tok.line, start: tok.col, length: tok.length, typ: semanticTypeComment})
			continue
		case semanticLexLBrace:
			spans = append(spans, semanticSpan{line: tok.line, start: tok.col, length: tok.length, typ: semanticTypeOperator})
			name := pending
			if name == "" {
				name = "unknown"
			}
			stack = append(stack, name)
			pending = ""
			lockedPending = false
			currentDirective = ""
			modePending = false
			prevSig = prevSigLBrace
			continue
		case semanticLexRBrace:
			spans = append(spans, semanticSpan{line: tok.line, start: tok.col, length: tok.length, typ: semanticTypeOperator})
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			pending = ""
			lockedPending = false
			currentDirective = ""
			modePending = false
			prevSig = prevSigRBrace
			continue
		case semanticLexSemicolon:
			spans = append(spans, semanticSpan{line: tok.line, start: tok.col, length: tok.length, typ: semanticTypeOperator})
			if !lockedPending {
				pending = ""
			}
			currentDirective = ""
			modePending = false
			prevSig = prevSigSemicolon
			continue
		case semanticLexOperator:
			spans = append(spans, semanticSpan{line: tok.line, start: tok.col, length: tok.length, typ: semanticTypeOperator})
			modePending = false
			prevSig = prevSigOther
			continue
		}

		block := "top"
		if len(stack) > 0 {
			block = stack[len(stack)-1]
		}
		statementStart := prevSig == prevSigBOF || prevSig == prevSigLBrace || prevSig == prevSigRBrace || prevSig == prevSigSemicolon

		switch tok.kind {
		case semanticLexString:
			tokenType := semanticTypeString
			if currentDirective == "provider" {
				tokenType = semanticTypeNamespace
			} else if modePending {
				tokenType = semanticTypeEnumMember
				modePending = false
			}
			spans = append(spans, semanticSpan{line: tok.line, start: tok.col, length: tok.length, typ: tokenType})
			prevSig = prevSigOther
		case semanticLexNumber:
			spans = append(spans, semanticSpan{line: tok.line, start: tok.col, length: tok.length, typ: semanticTypeNumber})
			prevSig = prevSigOther
		case semanticLexIdent:
			tokenType := classifyIdentifierType(tok.text, block, statementStart, modePending)
			if tokenType >= 0 {
				spans = append(spans, semanticSpan{line: tok.line, start: tok.col, length: tok.length, typ: tokenType})
			}
			if modePending {
				modePending = false
			}
			if statementStart && blockAllowsChildBlock(block, tok.text) {
				pending = tok.text
				lockedPending = blockDirectiveNeedsHeader(block, tok.text)
			}
			if statementStart {
				currentDirective = tok.text
				modePending = len(dslspec.ModesByDirectiveInBlock(tok.text, block)) > 0
			}
			prevSig = prevSigOther
		default:
			prevSig = prevSigOther
		}
	}

	return spans
}

func classifyIdentifierType(word, block string, statementStart, modePending bool) int {
	w := strings.TrimSpace(word)
	if w == "" {
		return -1
	}
	if modePending {
		return semanticTypeEnumMember
	}
	if w == "true" || w == "false" || w == "syntax" {
		return semanticTypeKeyword
	}
	if statementStart && blockAllowsChildBlock(block, w) {
		return semanticTypeKeyword
	}
	if directiveInBlock(w, block) {
		return semanticTypeProperty
	}
	return -1
}

func directiveInBlock(name, block string) bool {
	for _, d := range dslspec.DirectivesByBlock(block) {
		if d == name {
			return true
		}
	}
	return false
}

func encodeSemanticSpans(spans []semanticSpan) []uint32 {
	if len(spans) == 0 {
		return nil
	}
	data := make([]uint32, 0, len(spans)*5)
	prevLine := 0
	prevStart := 0
	for i, s := range spans {
		lineDelta := s.line
		startDelta := s.start
		if i > 0 {
			lineDelta = s.line - prevLine
			if lineDelta == 0 {
				startDelta = s.start - prevStart
			}
		}
		data = append(data, uint32(lineDelta), uint32(startDelta), uint32(s.length), uint32(s.typ), 0)
		prevLine = s.line
		prevStart = s.start
	}
	return data
}

func lexSemantic(input string) []semanticLexToken {
	state := semanticLexState{
		input: input,
		out:   make([]semanticLexToken, 0, len(input)/3),
	}
	for state.i < len(state.input) {
		if state.scanWhitespaceOrNewline() || state.scanComment() || state.scanSingleChar() || state.scanString() || state.scanIdentOrNumber() {
			continue
		}
		state.emit(semanticLexOther, string(state.input[state.i]), state.line, state.col)
		state.i++
		state.col++
	}
	return state.out
}

type semanticLexState struct {
	input string
	out   []semanticLexToken
	i     int
	line  int
	col   int
}

func (s *semanticLexState) emit(kind semanticLexKind, text string, line, col int) {
	s.out = append(s.out, semanticLexToken{
		kind:   kind,
		text:   text,
		line:   line,
		col:    col,
		length: len(text),
	})
}

func (s *semanticLexState) scanWhitespaceOrNewline() bool {
	ch := s.input[s.i]
	if ch == '\n' {
		s.line++
		s.col = 0
		s.i++
		return true
	}
	if ch == ' ' || ch == '\t' || ch == '\r' {
		s.col++
		s.i++
		return true
	}
	return false
}

func (s *semanticLexState) scanComment() bool {
	ch := s.input[s.i]
	if ch != '#' && (ch != '/' || s.i+1 >= len(s.input) || s.input[s.i+1] != '/') {
		return false
	}
	startLine, startCol, j := s.line, s.col, s.i
	for j < len(s.input) && s.input[j] != '\n' {
		j++
	}
	s.emit(semanticLexComment, s.input[s.i:j], startLine, startCol)
	s.col += j - s.i
	s.i = j
	return true
}

func (s *semanticLexState) scanSingleChar() bool {
	startLine, startCol := s.line, s.col
	switch s.input[s.i] {
	case '{':
		s.emit(semanticLexLBrace, "{", startLine, startCol)
	case '}':
		s.emit(semanticLexRBrace, "}", startLine, startCol)
	case ';':
		s.emit(semanticLexSemicolon, ";", startLine, startCol)
	case '=':
		s.emit(semanticLexOperator, "=", startLine, startCol)
	default:
		return false
	}
	s.i++
	s.col++
	return true
}

func (s *semanticLexState) scanString() bool {
	quote := s.input[s.i]
	if quote != '"' && quote != '\'' {
		return false
	}
	startLine, startCol := s.line, s.col
	j := s.i + 1
	for j < len(s.input) {
		if s.input[j] == '\\' && j+1 < len(s.input) {
			j += 2
			continue
		}
		if s.input[j] == quote {
			j++
			break
		}
		if s.input[j] == '\n' {
			break
		}
		j++
	}
	s.emit(semanticLexString, s.input[s.i:j], startLine, startCol)
	s.col += j - s.i
	s.i = j
	return true
}

func (s *semanticLexState) scanIdentOrNumber() bool {
	ch := s.input[s.i]
	startLine, startCol := s.line, s.col
	if isIdentStart(ch) {
		j := s.i + 1
		for j < len(s.input) && isIdentPart(s.input[j]) {
			j++
		}
		s.emit(semanticLexIdent, s.input[s.i:j], startLine, startCol)
		s.col += j - s.i
		s.i = j
		return true
	}
	if ch < '0' || ch > '9' {
		return false
	}
	j := s.i + 1
	for j < len(s.input) && s.input[j] >= '0' && s.input[j] <= '9' {
		j++
	}
	if j < len(s.input) && s.input[j] == '.' {
		k := j + 1
		for k < len(s.input) && s.input[k] >= '0' && s.input[k] <= '9' {
			k++
		}
		if k > j+1 {
			j = k
		}
	}
	s.emit(semanticLexNumber, s.input[s.i:j], startLine, startCol)
	s.col += j - s.i
	s.i = j
	return true
}
