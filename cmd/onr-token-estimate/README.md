# onr-token-estimate

`onr-token-estimate` reads http-relay simple dump logs, runs ONR's `usageestimate` logic for each complete record, and compares estimated tokens with upstream official usage.

## Run

From `relay/open-next-router`:

```bash
go run ./cmd/onr-token-estimate \
  --file /path/to/codex.log \
  --api responses \
  --model gpt-5.5
```

From this directory:

```bash
go run . -f /path/to/codex.log --api responses -m gpt-5.5
```

## Flags

```text
--file, -f          dump file path
--api              usageestimate API name
--route            route alias for API name
--model, -m        model name
--allow-truncated  allow truncated dump bodies
--debug-id         print extracted response output text for one dump id
--debug-preview    max characters printed for --debug-id, default 800
```

Use either `--api` or `--route`.

Supported API names:

```text
chat.completions
responses
claude.messages
embeddings
gemini.generateContent
gemini.streamGenerateContent
```

Supported route aliases:

```text
openai-chat
openai-chat-completions
openai-responses
anthropic-messages
claude-messages
gemini-generate-content
gemini-stream-generate-content
embeddings
```

## Input Format

The command expects records in http-relay `--simple-dump` shape:

```json
{
  "id": 1,
  "request": {
    "body": {
      "format": "json",
      "size": 19,
      "truncated": false,
      "content": {
        "ping": "pong"
      }
    }
  },
  "response": {
    "body": {
      "format": "json",
      "size": 11,
      "truncated": false,
      "content": {
        "ok": true,
        "usage": {
          "input_tokens": 10,
          "output_tokens": 2,
          "total_tokens": 12
        }
      }
    }
  }
}
```

Response SSE body is also supported:

```json
{
  "id": 2,
  "request": {
    "body": {
      "format": "empty",
      "size": 0,
      "truncated": false
    }
  },
  "response": {
    "body": {
      "format": "sse",
      "size": 71,
      "truncated": false,
      "events": [
        {
          "event": "response.output_text.delta",
          "data": {
            "type": "response.output_text.delta",
            "delta": "hello"
          }
        },
        {
          "event": "response.completed",
          "data": {
            "type": "response.completed",
            "response": {
              "usage": {
                "input_tokens": 10,
                "output_tokens": 2,
                "total_tokens": 12
              }
            }
          }
        }
      ]
    }
  }
}
```

Supported file containers:

Single JSON object:

```json
{ "id": 1, "request": { "body": { "format": "empty" } }, "response": { "body": { "format": "empty" } } }
```

JSON array:

```json
[
  { "id": 1, "request": { "body": { "format": "empty" } }, "response": { "body": { "format": "empty" } } },
  { "id": 2, "request": { "body": { "format": "empty" } }, "response": { "body": { "format": "empty" } } }
]
```

JSON stream, as produced by logs that append one object after another:

```json
{ "id": 1, "request": { "body": { "format": "empty" } }, "response": { "body": { "format": "empty" } } }
{ "id": 2, "request": { "body": { "format": "empty" } }, "response": { "body": { "format": "empty" } } }
```

Loose arrays with a leading `[` and missing final `]` are tolerated when entries can still be decoded.

## Output

The command prints one aligned table.

```text
status     idx  id  stage          in.actual  in.est  in.delta  out.actual  out.est  out.delta  reason
---------  ---  --  -------------  ---------  ------  --------  ----------  -------  ---------  -------------------------
estimated    4  4   estimate_both       9594    9485    -1.14%          60      125   +108.33%
skipped      6  8                                                                               incomplete upstream usage
summary entries=12 estimated=5 skipped=7
```

Columns:

- `in.actual` / `out.actual`: official upstream token usage extracted from the response.
- `in.est` / `out.est`: local estimate from `usageestimate.Estimate`.
- `in.delta` / `out.delta`: `(estimated - actual) / actual * 100`.
- `skipped`: record is not estimated. Most commonly the response has incomplete official usage.

## Debug

Use `--debug-id` when output estimation looks suspicious. It prints the response output text extracted from the matching dump record before the table.

```bash
go run . \
  -f /path/to/codex.log \
  --api responses \
  -m gpt-5.5 \
  --debug-id 29 \
  --debug-preview 1200
```

This is useful for checking whether SSE events are missing from `streamtext.ExtractDeltaText`.
