package apitransform

import (
	"encoding/json"
	"net/http"
	"strings"
)

const contentTypeJSON = "application/json"

// NormalizeResponseMapMode canonicalizes resp_map mode names so runtimes can
// share one supported-mode registry.
func NormalizeResponseMapMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

// SupportsResponseMapMode reports whether onr-core has a shared non-stream
// resp_map transform for the given mode.
func SupportsResponseMapMode(mode string) bool {
	switch NormalizeResponseMapMode(mode) {
	case "openai_responses_to_openai_chat",
		"anthropic_to_openai_chat",
		"gemini_to_openai_chat",
		"openai_to_anthropic_messages",
		"openai_to_gemini_chat",
		"openai_to_gemini_generate_content":
		return true
	default:
		return false
	}
}

// MapResponseBodyByMode runs the shared non-stream resp_map transform selected
// by mode and returns the transformed response object plus its downstream
// content type.
func MapResponseBodyByMode(mode string, body []byte) (map[string]any, string, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, "", err
	}
	out, err := MapResponseObjectByMode(mode, root)
	if err != nil {
		return nil, "", err
	}
	return out, contentTypeJSON, nil
}

func MapResponseObjectByMode(mode string, root map[string]any) (map[string]any, error) {
	switch NormalizeResponseMapMode(mode) {
	case "openai_responses_to_openai_chat":
		return mapResponseObjectViaBytes(root, MapOpenAIResponsesToChatCompletions)
	case "anthropic_to_openai_chat":
		return MapClaudeMessagesResponseToOpenAIChatCompletionsObject(root)
	case "gemini_to_openai_chat":
		return MapGeminiGenerateContentToOpenAIChatCompletionsResponseObject(root)
	case "openai_to_anthropic_messages":
		return mapResponseObjectViaBytes(root, MapOpenAIChatCompletionsToClaudeMessagesResponse)
	case "openai_to_gemini_chat", "openai_to_gemini_generate_content":
		return mapResponseObjectViaBytes(root, MapOpenAIChatCompletionsToGeminiGenerateContentResponse)
	default:
		return nil, unsupportedModeError("resp_map", mode)
	}
}

func mapResponseObjectViaBytes(
	root map[string]any,
	transform func([]byte) ([]byte, error),
) (map[string]any, error) {
	body, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	out, err := transform(body)
	if err != nil {
		return nil, err
	}
	var mapped map[string]any
	if err := json.Unmarshal(out, &mapped); err != nil {
		return nil, err
	}
	return mapped, nil
}

// TransformNonStreamResponseBody applies the shared non-stream resp_map flow:
// skip on upstream errors, decode the upstream body when needed, dispatch by
// mode, and return whether a transform was actually applied.
func TransformNonStreamResponseBody(
	statusCode int,
	mode string,
	body []byte,
	contentType string,
	contentEncoding string,
) (map[string]any, string, bool, error) {
	if statusCode >= http.StatusBadRequest {
		return nil, contentType, false, nil
	}
	decoded, _, err := DecodeResponseBody(body, contentEncoding)
	if err != nil {
		return nil, "", false, err
	}
	if !SupportsResponseMapMode(mode) {
		return nil, contentType, false, nil
	}
	outObj, outCT, err := MapResponseBodyByMode(mode, decoded)
	if err != nil {
		return nil, "", false, err
	}
	return outObj, outCT, true, nil
}
