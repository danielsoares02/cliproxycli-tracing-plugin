<img src="assets/icon.svg" width="64" height="64" alt="">

# CLIProxy Tracing

A native CLIProxyAPI plugin that exports every proxied request as an
OpenTelemetry trace over OTLP/HTTP, so CLIProxyAPI traffic shows up in PostHog,
Langfuse, Grafana Tempo, Jaeger, Datadog, Honeycomb, or any other OTLP backend.

Each inbound request becomes a trace: one server span for the request, and one
client span per upstream attempt, so retries and credential fallbacks are
visible as separate spans with their own status codes. Spans carry token
counts, latency, time to first token, provider, model, and, when enabled, the
prompt and completion.

There is no dashboard. The plugin reports to your tracing backend and nothing
else.

## Why another tracing plugin

The OTLP attribute vocabulary differs by backend, and getting it wrong fails
quietly: a span is accepted with a `200` and then never appears as a generation.
PostHog classifies a span as a generation only when it carries
`gen_ai.operation.name`, and reads content from `gen_ai.input.messages` and
`gen_ai.output.messages`. Langfuse reads its own `langfuse.observation.*`
attributes and prefers them over the generic conventions.

This plugin emits all of them on the same span, so one export serves either
backend with no per-backend configuration:

| Purpose | Attributes |
|---|---|
| Generation marker | `gen_ai.operation.name` = `chat`, `langfuse.observation.type` |
| Prompt | `gen_ai.input.messages`, `langfuse.observation.input`, `gen_ai.prompt` |
| Completion | `gen_ai.output.messages`, `langfuse.observation.output`, `gen_ai.completion` |
| Tool catalog | `gen_ai.tool.definitions` |
| Session | `gen_ai.conversation.id` |
| Tokens | `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, cached, reasoning |
| Models | `gen_ai.request.model`, `gen_ai.response.model` |
| Provenance | `cpa.content.prompt_source`, `cpa.content.completion_source`, `cpa.content.*_truncated` |

### Sessions and tool calls

`gen_ai.conversation.id` carries CLIProxyAPI's session identity, which groups
traces into one conversation in PostHog. The host derives that identity from the
client's session headers (`X-Claude-Code-Session-Id`, `Session-Id`,
`Thread-Id`, and others), so Claude Code and Codex sessions group automatically
without any client change.

Messages use the OTel GenAI `parts` shape rather than a flat `content` string:

```json
{"role":"assistant","parts":[
  {"type":"text","content":"Let me look."},
  {"type":"tool_call","id":"toolu_1","name":"read_file","arguments":{"path":"/etc/hosts"}},
  {"type":"reasoning","content":"The path is explicit."}]}
```

This is what makes tool calls appear in PostHog's Tools view. A flat string
renders as prose but is invisible to PostHog's tool-call extractor, so arguments
end up as unparseable text and the tool never reaches the Tools tab. Streamed
tool calls are reassembled from their argument fragments first, so a streamed
and a non-streamed call produce the same part. `gen_ai.tool.definitions` carries
the declared catalog from the request's `tools`, which is what lets the Tools
view distinguish a call it saw from one the agent could never have made.

## Setup

### PostHog

```yaml
plugins:
  configs:
    cliproxy-tracing:
      enabled: true
      endpoint: "https://us.i.posthog.com/i/v0/ai/otel"
      headers:
        Authorization: "Bearer phc_your_project_token"
      capture_content: true
```

Use `https://eu.i.posthog.com/i/v0/ai/otel` for an EU project. The token is the
project token from PostHog's settings, not a personal API key. Note that this
endpoint returns `200` even for an invalid token, so check the project, not the
status code, when confirming data arrives.

### Langfuse

```yaml
plugins:
  configs:
    cliproxy-tracing:
      enabled: true
      endpoint: "https://cloud.langfuse.com/api/public/otel/v1/traces"
      headers:
        Authorization: "Basic <base64 of pk-lf-...:sk-lf-...>"
        x-langfuse-ingestion-version: "4"
      capture_content: true
```

`x-langfuse-ingestion-version: 4` avoids a delay of up to ten minutes on
directly ingested OTLP data.

### OpenInference-compatible backends

The plugin emits OpenInference attributes alongside its existing GenAI and
Langfuse attributes; there is no separate switch for this. Compatible backends
include Arize Phoenix. The root span is marked `CHAIN`, each upstream attempt
is marked `LLM`, and available session, model and token fields are included.

```yaml
plugins:
  configs:
    cliproxy-tracing:
      enabled: true
      endpoint: "http://otel-backend:6006/v1/traces"
      protocol: protobuf
      capture_content: true
```

Use the backend's actual hostname and port; containers on the same Docker
network can use the service name. Configure `headers` if authentication is
required. `protocol` selects OTLP/HTTP encoding: `json` is the default and
`protobuf` sends `application/x-protobuf`. The encoding setting is independent
of the OpenInference attributes. Use Protobuf when the receiver requires it.

Content capture is opt-in. With `capture_content: true`, the last attempt
carries indexed input/output messages and the full captured history; tool calls
and results are represented within messages, not as separate tool-execution
spans. The root span carries `input.value` and `output.value` as bounded plain
text summaries: the most recent user text and the response text or tool names.
This supports session-turn views; tool-only follow-ups can still display the
preceding user message. Without captured content these fields are absent.
Captured prompts and responses are not automatically redacted.

Responses API `input_text` and `output_text` blocks are treated as text;
`function_call` and `function_call_output` history items are correlated by
`call_id`. Unknown blocks are retained as labeled text when possible. Previously
stored traces are not changed. The export batch size is calculated using the
JSON representation before Protobuf conversion, and Protobuf partial-success
responses are not decoded by this version.

### Any other OTLP/HTTP endpoint

Point `endpoint` at a collector's traces URL, e.g.
`http://127.0.0.1:4318/v1/traces`, and add whatever `headers` it requires. With
no endpoint set, the plugin sends nothing at all.

## Configuration

Settings live under `plugins.configs.cliproxy-tracing`. Every key is optional.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `endpoint` | string | empty | OTLP/HTTP traces URL. Empty disables export entirely. |
| `headers` | object | `{}` | Extra export headers, as a map or a list of `"Name: Value"`. |
| `protocol` | string | `json` | OTLP/HTTP wire format: `json` or `protobuf`. |
| `service_name` | string | `cliproxyapi` | OpenTelemetry `service.name`. |
| `capture_content` | boolean | `false` | Export prompt and completion text. |
| `max_content_chars` | integer | `32768` | Longest single message exported. |
| `max_trace_content_bytes` | integer | `524288` | Largest prompt or completion exported for one trace. |
| `require_complete` | boolean | `true` | Skip requests whose completion did not finish. |
| `sample_rate` | number | `1` | Fraction of traces to export, decided per trace. |
| `export_timeout_ms` | integer | `10000` | Per-attempt HTTP timeout. |
| `flush_interval_ms` | integer | `1000` | How often queued spans are exported. |
| `max_batch_spans` | integer | `512` | Spans per export request. |
| `max_batch_bytes` | integer | `3145728` | Encoded size cap for one export request. |
| `completion_grace_ms` | integer | `2000` | How long a finished request waits for late usage records. |

### Content capture

`capture_content` is off by default. When it is on, the plugin declares the
interceptor capabilities it needs and records:

- the client's request body, verbatim, as the prompt;
- the assembled streamed deltas, or the response body for a non-streaming call,
  as the completion.

The prompt is taken before translation, so it is the conversation the caller
actually wrote rather than a rewritten upstream payload. Prompts and
completions are **not** redacted: what the caller sent is what is exported, so a
credential pasted into a prompt leaves the machine with it. Scrub before export
if that matters for your deployment.

Every part of an agent conversation is kept, not just assistant prose. A
`tool_use` block becomes a `tool_call` part with its name and arguments, a
`tool_result` becomes a `tool_call_response` part correlated by id, and
`thinking` becomes a `reasoning` part, on both the streaming and non-streaming
paths and for every provider dialect. Streamed tool arguments arrive as
fragments and are joined in order. Plain text parts are exported as their text
alone, with no type prefix.

Three limits apply. Content above `max_content_chars` per message is truncated
with a marker. A prompt or completion above `max_trace_content_bytes` is clipped
too: the oldest turns of a long conversation are dropped and the first remaining
message is marked, so the most recent exchange, which is the one being traced,
always survives. Bodies above 4 MiB and completions above 2 MiB are truncated at
capture time. The span records every truncation. `require_complete` drops
requests that did not finish, so the export holds whole exchanges rather than
fragments.

### Export sizing

Backends enforce a body limit on an ingest request and reject an oversized one
whole: PostHog answers `413` above 4 MiB and discards every span in the request,
not just the large one. A batch therefore has to be split before it gets there.

Two things keep it under the limit. Full conversation content is attached to
the last attempt, not every retry; the root additionally receives bounded turn
summaries. When there are no attempts the root carries the conversation. Batches split on
encoded bytes as well as span count, with a 3 MiB default budget that leaves
headroom under the 4 MiB limit. A batch that is still refused is halved and
retried rather than dropped.

Transient failures, meaning a network error, a 5xx, or a `429`, return their
spans to a bounded retry queue and are resent after a backoff, so a backend
restart does not lose the traffic that happened during it. Queue overflow drops
the oldest spans and the count is reported.

### What is never captured

Request and response headers, credentials, and the client API key. The plugin
never reads the key: it does not appear in any span attribute.

## Latency

The plugin runs on the request path only as far as the host forces it to.

- The request body is copied once, not parsed.
- Each stream chunk is scanned for a delta keyword and, when one is present,
  unmarshalled once to extract its text.
- Every other step, including prompt parsing, span building, batching, and the
  network export, runs on a single background goroutine.

Batching and retry happen there too, so a slow or unreachable backend never
blocks a request. A failing endpoint backs off exponentially and its spans are
dropped rather than queued without limit, and the health of the exporter is
reported to the host log.

## Build

Requires Go 1.24+ and a C compiler (cgo). For a container-only Linux build, run
`docker run --rm -v "$PWD:/src" -w /src golang:1.24-bookworm bash -c './build.sh'`
from the project directory.

```sh
./build.sh                     # -> cliproxy-tracing-v0.2.2.so (linux/amd64)
ARCH=arm64 ./build.sh          # native arm64 build
./build-release.sh             # plugin-store artifacts in dist/
```

`./build-release.sh` produces one zip per platform plus `checksums.txt`. All
six store platforms are mandatory (Darwin, Linux and Windows, each on amd64 and
arm64), so the script fails rather than skipping a platform whose compiler is
missing. Tagging `v<version>` triggers the release workflow, which builds each
platform on a matching runner and publishes only after the asset matrix check
passes, including an architecture check on every packaged library.

## Privacy

Capture is off unless you turn it on. With it off, the plugin exports request
metadata only: models, providers, token counts, latency, time to first token,
status codes, and a per-trace identifier. No prompt or completion text is read
from the request or response at all, and the interceptors return nothing.

## License

MIT. See [LICENSE](LICENSE).
