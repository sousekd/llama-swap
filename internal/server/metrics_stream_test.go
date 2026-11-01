package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/hint"
	"github.com/mostlygeek/llama-swap/internal/logmon"
	"github.com/mostlygeek/llama-swap/internal/server/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const observedChatOutput = `data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"hello"}}]}` + "\n"
const observedChatUsage = `data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`

func TestServer_ChatStreamFallback_Rates(t *testing.T) {
	tests := []struct {
		name       string
		extra      string
		wantPrompt float64
		wantOutput float64
	}{
		{"missing rates", "", 40, 8},
		{"reported rates", `,"prompt_tokens_per_second":35,"generation_tokens_per_second":17`, 35, 17},
		{"reported zero", `,"prompt_tokens_per_second":0,"generation_tokens_per_second":0`, 0, 0},
		{"missing decode", `,"prompt_tokens_per_second":35`, 35, 8},
		{"missing prompt", `,"generation_tokens_per_second":17`, 40, 17},
		{"null rates", `,"prompt_tokens_per_second":null,"generation_tokens_per_second":null`, 40, 8},
		{"non-numeric rates", `,"prompt_tokens_per_second":"unknown","generation_tokens_per_second":false`, 40, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			final := strings.Replace(observedChatUsage, `"prompt_tokens":10`, `"prompt_tokens":10`+tt.extra, 1)
			body := observedChatOutput + observedChatOutput + final + "\n"
			u, err := usage.Extract(hint.Unknown, "/v1/chat/completions", "text/event-stream", []byte(body), time.Now())
			require.NoError(t, err)
			before := u
			o := newChatStreamObserver("/v1/chat/completions")
			o.observe([]byte(observedChatOutput), 200*time.Millisecond)
			o.observe([]byte(observedChatOutput+final+"\n"), 700*time.Millisecond)
			o.applyRateFallback(&u.Tokens)
			assert.Equal(t, tt.wantPrompt, u.Tokens.PromptPerSecond)
			assert.Equal(t, tt.wantOutput, u.Tokens.TokensPerSecond)
			before.Tokens.PromptPerSecond, before.Tokens.TokensPerSecond = tt.wantPrompt, tt.wantOutput
			assert.Equal(t, before, u, "only rates may change")
		})
	}
}

func TestServer_ChatStreamFallback_EngineIndependent(t *testing.T) {
	for _, upstream := range []hint.Upstream{
		hint.Unknown, hint.OpenAICompatible, hint.OpenAI, hint.OpenRouter,
		hint.LlamaSwap, hint.VLLM, hint.LlamaServer, hint.OMLX, hint.TabbyAPI,
		hint.Kyojin, hint.Radiance, hint.Halogen,
	} {
		t.Run(string(upstream), func(t *testing.T) {
			u, err := usage.Extract(upstream, "/v1/chat/completions", "text/event-stream",
				[]byte(observedChatOutput+observedChatUsage+"\n"), time.Now())
			require.NoError(t, err)
			o := newChatStreamObserver("/v1/chat/completions")
			o.observe([]byte(observedChatOutput), 200*time.Millisecond)
			o.observe([]byte(observedChatOutput+observedChatUsage), 700*time.Millisecond)
			o.applyRateFallback(&u.Tokens)
			assert.Equal(t, 40.0, u.Tokens.PromptPerSecond)
			assert.Equal(t, 8.0, u.Tokens.TokensPerSecond)
			assert.Equal(t, 1, u.Tokens.ReasoningTokens)
		})
	}
}

func TestServer_ChatStreamFallback_NativeNormalization(t *testing.T) {
	for _, tt := range []struct {
		name, extra string
		upstream    hint.Upstream
		prompt, gen float64
		input       int
	}{
		{"llama counts and missing decode", `,"timings":{"prompt_n":8,"cache_n":2,"predicted_n":5,"prompt_per_second":80,"draft_n":3,"draft_n_accepted":2}`, hint.LlamaServer, 80, 8, 10},
		{"vllm excludes queue time", `,"metrics":{"time_to_first_token_ms":200,"queue_time_ms":100,"mean_itl_ms":20,"speculative_decoding":{"num_draft_tokens":3,"num_accepted_draft_tokens":2}}`, hint.VLLM, 80, 50, 10},
	} {
		t.Run(tt.name, func(t *testing.T) {
			final := strings.TrimSuffix(observedChatUsage, "}") + tt.extra + "}\n"
			u, err := usage.Extract(tt.upstream, "/v1/chat/completions", "text/event-stream",
				[]byte(observedChatOutput+final), time.Now())
			require.NoError(t, err)
			o := newChatStreamObserver("/v1/chat/completions")
			o.observe([]byte(observedChatOutput), 200*time.Millisecond)
			o.observe([]byte(observedChatOutput+final), 700*time.Millisecond)
			o.applyRateFallback(&u.Tokens)
			assert.Equal(t, tt.prompt, u.Tokens.PromptPerSecond)
			assert.Equal(t, tt.gen, u.Tokens.TokensPerSecond)
			assert.Equal(t, tt.input, u.Tokens.InputTokens)
			assert.Equal(t, 2, u.Tokens.CachedTokens)
			assert.Equal(t, 3, u.Tokens.DraftTokens)
			assert.Equal(t, 2, u.Tokens.DraftAccTokens)
		})
	}
}

func TestServer_ChatStreamFallback_Eligibility(t *testing.T) {
	for _, tt := range []struct {
		name, final          string
		first, last          time.Duration
		input, output, cache int
		prompt, gen          float64
	}{
		{"normal", observedChatUsage, 200 * time.Millisecond, 700 * time.Millisecond, 10, 5, 2, 40, 8},
		{"missing final", "", 200 * time.Millisecond, 700 * time.Millisecond, 10, 5, 2, -1, -1},
		{"intermediate usage", `data: {"object":"chat.completion.chunk","choices":[{"delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`, 200 * time.Millisecond, 700 * time.Millisecond, 10, 5, 2, -1, -1},
		{"missing count", `data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10}}`, 200 * time.Millisecond, 700 * time.Millisecond, 10, 5, 2, -1, -1},
		{"invalid count", strings.Replace(observedChatUsage, `"completion_tokens":5`, `"completion_tokens":null`, 1), 200 * time.Millisecond, 700 * time.Millisecond, 10, 5, 2, -1, -1},
		{"fractional count", strings.Replace(observedChatUsage, `"completion_tokens":5`, `"completion_tokens":1.5`, 1), 200 * time.Millisecond, 700 * time.Millisecond, 10, 5, 2, -1, -1},
		{"batched output", observedChatUsage, 200 * time.Millisecond, 200 * time.Millisecond, 10, 5, 2, 40, -1},
		{"zero first interval", observedChatUsage, 0, 500 * time.Millisecond, 10, 5, 2, -1, 8},
		{"one token", strings.Replace(observedChatUsage, `"completion_tokens":5`, `"completion_tokens":1`, 1), 200 * time.Millisecond, 700 * time.Millisecond, 10, 1, 2, 40, -1},
		{"final zero overrides partial counts", strings.Replace(observedChatUsage, `"completion_tokens":5`, `"completion_tokens":0`, 1), 200 * time.Millisecond, 700 * time.Millisecond, 10, 5, 2, 40, -1},
		{"unknown cache", observedChatUsage, 200 * time.Millisecond, 700 * time.Millisecond, 10, 5, -1, 50, 8},
		{"excessive cache", observedChatUsage, 200 * time.Millisecond, 700 * time.Millisecond, 10, 5, 12, -1, 8},
		{"no output", observedChatUsage, -1, -1, 10, 5, 2, -1, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := newChatStreamObserver("/v1/chat/completions")
			o.firstOutput, o.lastOutput = tt.first, tt.last
			o.observe([]byte(tt.final), time.Second)
			tokens := TokenMetrics{InputTokens: tt.input, OutputTokens: tt.output, CachedTokens: tt.cache, PromptPerSecond: -1, TokensPerSecond: -1}
			o.applyRateFallback(&tokens)
			assert.Equal(t, tt.prompt, tokens.PromptPerSecond)
			assert.Equal(t, tt.gen, tokens.TokensPerSecond)
		})
	}
}

func TestServer_ChatStreamObserver_OutputAndSplitLines(t *testing.T) {
	for _, delta := range []string{
		`{"content":"text"}`, `{"reasoning_content":"think"}`, `{"reasoning":"think"}`,
		`{"tool_calls":[{"index":0}]}`, `{"function_call":{"arguments":"{}"}}`,
	} {
		t.Run(delta, func(t *testing.T) {
			o := newChatStreamObserver("/v1/chat/completions")
			o.observe([]byte(": comment\ndata: [DONE]\ndata: invalid\n"+
				`data: {"choices":[{"delta":{"reasoning_content":"Loading model"}}]}`+"\n"+
				`data: {"object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"}}]}`+"\n"), 100*time.Millisecond)
			line := `data: {"object":"chat.completion.chunk","choices":[{"delta":{}},{"delta":` + delta + `}]}`
			o.observe([]byte(line[:20]), 150*time.Millisecond)
			o.observe([]byte(line[20:]+"\n"), 200*time.Millisecond)
			o.observe([]byte(observedChatOutput+observedChatOutput), 700*time.Millisecond)
			assert.Equal(t, 200*time.Millisecond, o.firstOutput)
			assert.Equal(t, 700*time.Millisecond, o.lastOutput)
		})
	}
}

func TestServer_ChatStreamFallback_Middleware(t *testing.T) {
	for _, tt := range []struct {
		name, path                         string
		stream, compressed, captures, peer bool
		status                             int
		estimate                           bool
	}{
		{"chat", "/v1/chat/completions", true, false, false, false, 200, true},
		{"versionless", "/v/chat/completions", true, false, false, false, 200, true},
		{"direct upstream", "/upstream/m/v1/chat/completions", true, false, false, false, 200, true},
		{"peer and capture", "/v1/chat/completions", true, false, true, true, 200, true},
		{"responses", "/v1/responses", true, false, false, false, 200, false},
		{"messages", "/v1/messages", true, false, false, false, 200, false},
		{"json", "/v1/chat/completions", false, false, false, false, 200, false},
		{"compressed", "/v1/chat/completions", true, true, false, false, 200, false},
		{"error", "/v1/chat/completions", true, false, false, false, 500, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			captureMB := 0
			if tt.captures {
				captureMB = 1
			}
			mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, captureMB)
			cfg := config.Config{Models: map[string]config.ModelConfig{"m": {}}}
			handler := CreateMetricsMiddleware(mm, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h := hint.From(r.Context())
				h.Route, h.Upstream = hint.RouteLocal, hint.Unknown
				if tt.peer {
					h.Route, h.PeerID, h.Upstream = hint.RoutePeer, "test-peer", hint.OpenAI
				}
				rec := w.(*responseBodyCopier)
				rec.start = time.Now().Add(-time.Second)
				contentType := "application/json"
				body := `{"usage":{"prompt_tokens":10,"completion_tokens":5}}`
				if tt.stream {
					contentType, body = "text/event-stream", observedChatOutput+observedChatUsage+"\n"
				}
				w.Header().Set("Content-Type", contentType)
				if tt.compressed {
					var buf bytes.Buffer
					zw := gzip.NewWriter(&buf)
					_, err := zw.Write([]byte(body))
					require.NoError(t, err)
					require.NoError(t, zw.Close())
					body = buf.String()
					w.Header().Set("Content-Encoding", "gzip")
				}
				w.WriteHeader(tt.status)
				_, err := io.WriteString(w, body)
				require.NoError(t, err)
				if rec.observer != nil {
					rec.observer.firstOutput, rec.observer.lastOutput = 200*time.Millisecond, 700*time.Millisecond
				}
			}))
			r := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(`{"model":"m"}`))
			r.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(httptest.NewRecorder(), r)
			entries := metricsEntries(t, mm)
			require.Len(t, entries, 1)
			tokens := entries[0].Tokens
			if tt.estimate {
				assert.Equal(t, 40.0, tokens.PromptPerSecond)
				assert.Equal(t, 8.0, tokens.TokensPerSecond)
			} else {
				assert.NotEqual(t, 40.0, tokens.PromptPerSecond)
				assert.NotEqual(t, 8.0, tokens.TokensPerSecond)
			}
			assert.Equal(t, tt.captures, entries[0].HasCapture)
			if tt.peer {
				assert.Equal(t, "peer", entries[0].Route)
				assert.Equal(t, "test-peer", entries[0].PeerID)
			}
		})
	}
}

type chatStreamPartialWriter struct {
	header http.Header
	n      int
	err    error
}

func (w *chatStreamPartialWriter) Header() http.Header       { return w.header }
func (w *chatStreamPartialWriter) WriteHeader(int)           {}
func (w *chatStreamPartialWriter) Write([]byte) (int, error) { return w.n, w.err }

func TestServer_ChatStreamObserver_ResponseWrites(t *testing.T) {
	for _, tt := range []struct {
		name, contentType, encoding string
		writer                      http.ResponseWriter
		observe                     bool
	}{
		{"SSE", "text/event-stream; charset=utf-8", "", httptest.NewRecorder(), true},
		{"JSON", "application/json", "", httptest.NewRecorder(), false},
		{"gzip", "text/event-stream", "gzip", httptest.NewRecorder(), false},
		{"short write", "text/event-stream", "", &chatStreamPartialWriter{header: make(http.Header), n: 1}, false},
		{"write error", "text/event-stream", "", &chatStreamPartialWriter{header: make(http.Header), err: io.ErrClosedPipe}, false},
		{"upgrade", "text/event-stream", "", httptest.NewRecorder(), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.writer.Header().Set("Content-Type", tt.contentType)
			tt.writer.Header().Set("Content-Encoding", tt.encoding)
			w := newBodyCopier(tt.writer)
			w.observer = newChatStreamObserver("/v1/chat/completions")
			w.start = time.Now().Add(-time.Second)
			if tt.name == "upgrade" {
				w.WriteHeader(http.StatusSwitchingProtocols)
			}
			w.Write([]byte(observedChatOutput))
			if tt.observe {
				require.NotNil(t, w.observer)
				assert.GreaterOrEqual(t, w.observer.firstOutput, time.Second)
			} else if w.observer != nil {
				assert.Equal(t, time.Duration(-1), w.observer.firstOutput)
			}
		})
	}
}
