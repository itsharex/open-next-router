package dsllang

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	dslconfig "github.com/r9s-ai/open-next-router/onr-core/pkg/dslconfig"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslspec"
)

var scannerErrRe = regexp.MustCompile(`^(.+):(\d+):(\d+):\s*(.+)$`)
var providerNameRe = regexp.MustCompile(`(?m)\bprovider\s+"([^"]+)"`)
var semanticDirectiveMustBeRe = regexp.MustCompile(`:\s*[^\s]+\s+([a-z_][a-z0-9_]*)\s+must\s+be\b`)
var semanticUnsupportedModeRe = regexp.MustCompile(`unsupported\s+([a-z_][a-z0-9_]*)\s+mode\b`)
var semanticUnsupportedDirectiveRe = regexp.MustCompile(`unsupported\s+([a-z_][a-z0-9_]*)\b`)

func collectDiagnostics(uri, text string) []Diagnostic {
	out := make([]Diagnostic, 0, 8)
	out = append(out, analyze(text)...)
	out = append(out, analyzeSemanticModes(text)...)
	out = append(out, analyzeSemantic(uri, text)...)
	return dedupeDiagnostics(out)
}

func analyzeSemantic(uri, text string) []Diagnostic {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	p, cleanup, validate, err := writeTempValidationFile(uri, text)
	if err != nil {
		return []Diagnostic{newDiagnostic(0, 0, "semantic validation setup failed: "+err.Error())}
	}
	defer cleanup()

	err = validate(p)
	if err == nil {
		return nil
	}

	msg := err.Error()
	if strings.Contains(msg, "declares provider") && strings.Contains(msg, "expected") {
		// Usually a transient edit-state mismatch, not a semantic DSL problem.
		return nil
	}
	if shouldSuppressSemanticModeDiagnostic(msg) {
		return nil
	}
	var issue *dslconfig.ValidationIssue
	if errors.As(err, &issue) {
		if line, col, ok := semanticDirectivePositionWithScope(text, issue.Directive, issue.Scope); ok {
			return []Diagnostic{newDiagnostic(line, col, msg)}
		}
	}

	if m := scannerErrRe.FindStringSubmatch(msg); len(m) == 5 {
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		return []Diagnostic{newDiagnostic(max(line-1, 0), max(col-1, 0), strings.TrimSpace(m[4]))}
	}
	return []Diagnostic{diagnosticFromSemanticMessage(text, msg)}
}

func analyzeSemanticModes(text string) []Diagnostic {
	toks := lex(text)
	if len(toks) == 0 {
		return nil
	}
	out := make([]Diagnostic, 0, 8)
	stack := make([]string, 0, 8)
	pending := ""
	lockedPending := false
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		block := "top"
		if len(stack) > 0 {
			block = stack[len(stack)-1]
		}

		switch tok.kind {
		case tokIdent:
			if !isStatementStart(toks, i) {
				continue
			}
			allowed := allowedModesForDirectiveInBlock(tok.text, block)
			if len(allowed) == 0 {
				if blockAllowsChildBlock(block, tok.text) {
					pending = tok.text
					lockedPending = blockDirectiveNeedsHeader(block, tok.text)
				}
				continue
			}
			if dslspec.DirectiveHasDynamicModeRegistryInBlock(tok.text, block) {
				continue
			}
			modeTok, ok := nextModeToken(toks, i+1)
			if !ok {
				continue
			}
			mode := normalizeModeToken(modeTok)
			if mode == "" {
				continue
			}
			if _, ok := allowed[strings.ToLower(mode)]; ok {
				continue
			}
			out = append(out, newDiagnostic(modeTok.line, modeTok.col, fmt.Sprintf("unsupported %s mode %q", tok.text, mode)))
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
		default:
			continue
		}
	}
	return out
}

func isStatementStart(toks []token, idx int) bool {
	if idx == 0 {
		return true
	}
	prev := toks[idx-1]
	switch prev.kind {
	case tokLBrace, tokSemicolon, tokRBrace:
		return true
	default:
		return false
	}
}

func nextModeToken(toks []token, idx int) (token, bool) {
	for i := idx; i < len(toks); i++ {
		switch toks[i].kind {
		case tokIdent, tokString:
			return toks[i], true
		case tokSemicolon, tokLBrace, tokRBrace, tokEOF:
			return token{}, false
		}
	}
	return token{}, false
}

func normalizeModeToken(tok token) string {
	if tok.kind == tokString {
		return strings.Trim(strings.TrimSpace(tok.text), "\"'")
	}
	return strings.TrimSpace(tok.text)
}

func allowedModesForDirectiveInBlock(d, block string) map[string]struct{} {
	return setFromSlice(dslspec.ModesByDirectiveInBlock(d, block))
}

func setFromSlice(v []string) map[string]struct{} {
	out := make(map[string]struct{}, len(v))
	for _, s := range v {
		out[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
	}
	return out
}

func writeTempValidationFile(uri, content string) (string, func(), func(string) error, error) {
	dir, err := os.MkdirTemp("", "onr-lsp-*")
	if err != nil {
		return "", nil, nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	if p, validate, ok, err := writeContextualValidationFile(dir, uri, content); err != nil {
		cleanup()
		return "", nil, nil, err
	} else if ok {
		return p, cleanup, validate, nil
	}

	validate := func(path string) error {
		_, err := dslconfig.ValidateProvidersFile(path)
		return err
	}
	filename := validationFilename(uri, content)
	if hasProvider, err := documentHasProvider(uri, content); err == nil && hasProvider {
		validate = func(path string) error {
			_, err := dslconfig.ValidateProviderFile(path)
			return err
		}
	}
	p := filepath.Join(dir, filename)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		cleanup()
		return "", nil, nil, fmt.Errorf("write temp validation file: %w", err)
	}
	return p, cleanup, validate, nil
}

func writeContextualValidationFile(tempRoot, uri, content string) (string, func(string) error, bool, error) {
	actualPath, ok := filePathFromURI(uri)
	if !ok {
		return "", nil, false, nil
	}
	role, cfgRoot := detectValidationContext(actualPath)
	if role == "" || cfgRoot == "" {
		return "", nil, false, nil
	}

	switch role {
	case "provider":
		dst := filepath.Join(tempRoot, "providers", filepath.Base(actualPath))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", nil, false, fmt.Errorf("create temp providers dir: %w", err)
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return "", nil, false, fmt.Errorf("write temp provider file: %w", err)
		}
		if err := copyFileIfExists(filepath.Join(cfgRoot, "onr.conf"), filepath.Join(tempRoot, "onr.conf")); err != nil {
			return "", nil, false, err
		}
		if err := copyDirIfExists(filepath.Join(cfgRoot, "modes"), filepath.Join(tempRoot, "modes")); err != nil {
			return "", nil, false, err
		}
		return dst, func(path string) error {
			_, err := dslconfig.ValidateProviderFile(path)
			return err
		}, true, nil
	case "mode":
		if err := copyDirIfExists(filepath.Join(cfgRoot, "providers"), filepath.Join(tempRoot, "providers")); err != nil {
			return "", nil, false, err
		}
		if err := copyDirIfExists(filepath.Join(cfgRoot, "modes"), filepath.Join(tempRoot, "modes")); err != nil {
			return "", nil, false, err
		}
		if err := copyFileIfExists(filepath.Join(cfgRoot, "onr.conf"), filepath.Join(tempRoot, "onr.conf")); err != nil {
			return "", nil, false, err
		}
		dst := filepath.Join(tempRoot, "modes", filepath.Base(actualPath))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", nil, false, fmt.Errorf("create temp modes dir: %w", err)
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return "", nil, false, fmt.Errorf("write temp mode file: %w", err)
		}
		validatePath := filepath.Join(tempRoot, "onr.conf")
		if _, err := os.Stat(validatePath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", nil, false, nil
			}
			return "", nil, false, fmt.Errorf("stat temp onr.conf: %w", err)
		}
		return validatePath, func(path string) error {
			_, err := dslconfig.ValidateProvidersFile(path)
			return err
		}, true, nil
	case "onr.conf":
		if err := copyDirIfExists(filepath.Join(cfgRoot, "providers"), filepath.Join(tempRoot, "providers")); err != nil {
			return "", nil, false, err
		}
		if err := copyDirIfExists(filepath.Join(cfgRoot, "modes"), filepath.Join(tempRoot, "modes")); err != nil {
			return "", nil, false, err
		}
		dst := filepath.Join(tempRoot, "onr.conf")
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return "", nil, false, fmt.Errorf("write temp onr.conf: %w", err)
		}
		return dst, func(path string) error {
			_, err := dslconfig.ValidateProvidersFile(path)
			return err
		}, true, nil
	case "providers.conf":
		if err := copyDirIfExists(filepath.Join(cfgRoot, "modes"), filepath.Join(tempRoot, "modes")); err != nil {
			return "", nil, false, err
		}
		dst := filepath.Join(tempRoot, "providers.conf")
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return "", nil, false, fmt.Errorf("write temp providers.conf: %w", err)
		}
		return dst, func(path string) error {
			_, err := dslconfig.ValidateProvidersFile(path)
			return err
		}, true, nil
	default:
		return "", nil, false, nil
	}
}

func detectValidationContext(actualPath string) (string, string) {
	p := filepath.Clean(strings.TrimSpace(actualPath))
	if p == "" {
		return "", ""
	}
	base := filepath.Base(p)
	parent := filepath.Base(filepath.Dir(p))
	switch {
	case base == "onr.conf":
		return "onr.conf", filepath.Dir(p)
	case base == "providers.conf":
		return "providers.conf", filepath.Dir(p)
	case parent == "providers":
		return "provider", filepath.Dir(filepath.Dir(p))
	case parent == "modes":
		return "mode", filepath.Dir(filepath.Dir(p))
	default:
		return "", ""
	}
}

func filePathFromURI(uri string) (string, bool) {
	if strings.TrimSpace(uri) == "" {
		return "", false
	}
	u, err := url.Parse(uri)
	if err == nil && u.Scheme == "file" {
		if u.Path == "" {
			return "", false
		}
		return filepath.FromSlash(u.Path), true
	}
	if filepath.IsAbs(uri) {
		return uri, true
	}
	return "", false
}

func copyFileIfExists(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %q: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create dir for %q: %w", dst, err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", dst, err)
	}
	return nil
}

func copyDirIfExists(srcDir, dstDir string) error {
	info, err := os.Stat(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %q: %w", srcDir, err)
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dstPath, b, 0o644)
	})
}

func validationFilename(uri, content string) string {
	providerName := extractProviderName(content)
	if providerName == "" {
		providerName = providerNameFromURI(uri)
	}
	if providerName == "" {
		providerName = "untitled"
	}
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	providerName = strings.ReplaceAll(providerName, " ", "-")
	if filepath.Ext(providerName) == "" {
		providerName += ".conf"
	}
	return providerName
}

func documentHasProvider(uri, content string) (bool, error) {
	_, hasProvider, err := dslconfig.FindProviderNameOptional(providerNameFromURI(uri), content)
	if err != nil {
		return false, err
	}
	return hasProvider, nil
}

func extractProviderName(content string) string {
	m := providerNameRe.FindStringSubmatch(content)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func providerNameFromURI(uri string) string {
	if uri == "" {
		return ""
	}
	u, err := url.Parse(uri)
	if err == nil && u.Scheme == "file" {
		base := path.Base(u.Path)
		return strings.TrimSuffix(base, path.Ext(base))
	}
	base := filepath.Base(uri)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func dedupeDiagnostics(in []Diagnostic) []Diagnostic {
	if len(in) <= 1 {
		return in
	}
	seen := map[string]struct{}{}
	out := make([]Diagnostic, 0, len(in))
	for _, d := range in {
		key := fmt.Sprintf("%d:%d:%s", d.Range.Start.Line, d.Range.Start.Character, d.Message)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, d)
	}
	return out
}

func newDiagnostic(line, col int, msg string) Diagnostic {
	return Diagnostic{
		Range: Range{
			Start: Position{Line: line, Character: col},
			End:   Position{Line: line, Character: col + 1},
		},
		Severity: 1,
		Source:   "onr-lsp",
		Message:  msg,
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func diagnosticFromSemanticMessage(text, msg string) Diagnostic {
	if line, col, ok := semanticDirectivePosition(text, msg); ok {
		return newDiagnostic(line, col, msg)
	}
	return newDiagnostic(0, 0, msg)
}

func semanticDirectivePosition(text, msg string) (int, int, bool) {
	directive := semanticDirectiveFromMessage(msg)
	if directive == "" {
		return 0, 0, false
	}
	return semanticDirectivePositionWithScope(text, directive, "")
}

func semanticDirectivePositionWithScope(text, directive, scope string) (int, int, bool) {
	blockHint := blockHintFromScope(scope)
	toks := lex(text)
	stack := make([]string, 0, 8)
	pending := ""
	lockedPending := false
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
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
			if tok.text == directive && isStatementStart(toks, i) {
				if blockHint == "" || block == blockHint {
					return tok.line, tok.col, true
				}
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
	return 0, 0, false
}

func blockHintFromScope(scope string) string {
	s := strings.TrimSpace(scope)
	if s == "" {
		return ""
	}
	if strings.Contains(s, ".auth.oauth") {
		return "auth"
	}
	segments := strings.Split(s, ".")
	for i := len(segments) - 1; i >= 0; i-- {
		seg := scopeSegmentBase(segments[i])
		if dslspec.IsBlockDirective(seg) {
			return seg
		}
	}
	return ""
}

func scopeSegmentBase(seg string) string {
	s := strings.TrimSpace(seg)
	if i := strings.IndexByte(s, '['); i >= 0 {
		return s[:i]
	}
	return s
}

func semanticDirectiveFromMessage(msg string) string {
	if m := semanticDirectiveMustBeRe.FindStringSubmatch(strings.ToLower(msg)); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := semanticUnsupportedModeRe.FindStringSubmatch(strings.ToLower(msg)); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := semanticUnsupportedDirectiveRe.FindStringSubmatch(strings.ToLower(msg)); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func shouldSuppressSemanticModeDiagnostic(msg string) bool {
	m := semanticUnsupportedModeRe.FindStringSubmatch(strings.ToLower(msg))
	if len(m) != 2 {
		return false
	}
	return dslspec.DirectiveHasDynamicModeRegistry(strings.TrimSpace(m[1]))
}
