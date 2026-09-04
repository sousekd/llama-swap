---
title: Observability, storage and Activity
summary: Use logs, metrics, captures and the Activity view to diagnose requests and retain useful history.
category: guides
tags: [operations, logs, metrics, activity, captures]
config_keys: [logLevel, logToStdout, metricsMaxInMemory, captureBuffer]
updated: 2026-09-04
---

# Observability, storage and Activity

Use the Activity view for recent request timing and model events, logs for
process and proxy failures, and metrics for trends. `metricsMaxInMemory` and
`captureBuffer` bound retained in-memory data; increase them only when the
memory cost is acceptable.

```yaml
logLevel: debug
logToStdout: true
metricsMaxInMemory: 1000
captureBuffer: 100
```

Do not put secrets in captures or debug logs. Reduce retention after diagnosing
an issue.

## Streamed Chat speed estimates

Activity prefers timing metrics reported by the model server. When a streamed
Chat Completions response has no recognized native timing metric, llama-swap
may estimate the missing prompt or generation rate from response-write timing.
The stream must include a standard final usage chunk with exact token counts.
Some model servers require clients to request this with
`stream_options.include_usage`.

Prompt speed uses uncached input tokens and the time from request dispatch to
the first output-bearing chunk. It therefore includes queueing, model loading,
and upstream latency. Generation speed spreads the remaining output tokens
across the time between the first and last output-bearing chunks.

These rates are request-level estimates. Server and proxy buffering, chunk
batching, and multiple choices can reduce their accuracy. Compressed,
incomplete, usage-free, or single-write streams may leave one or both rates
unavailable. llama-swap does not rewrite requests to enable usage reporting.
