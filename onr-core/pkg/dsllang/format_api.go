package dsllang

// FormatOptions controls indentation behavior for ONR DSL formatting.
type FormatOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

// FormatText formats ONR DSL text using stable indentation rules.
func FormatText(text string, opts FormatOptions) string {
	return formatDocument(text, formattingOptions(opts))
}
