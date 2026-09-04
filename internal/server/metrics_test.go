package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mostlygeek/llama-swap/internal/chain"
	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/logmon"
	"github.com/mostlygeek/llama-swap/internal/swaputil"
	"github.com/tidwall/gjson"
)

func TestServer_ParseMetrics_ChatCompletions(t *testing.T) {
	body := `{"usage":{"prompt_tokens":12,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":4}}}`
	parsed := gjson.Parse(body)
	entry, err := parseMetrics("m", time.Now(), parsed.Get("usage"), parsed.Get("timings"), parsed.Get("metrics"))
	if err != nil {
		t.Fatalf("parseMetrics: %v", err)
	}
	if entry.Tokens.InputTokens != 12 || entry.Tokens.OutputTokens != 7 || entry.Tokens.CachedTokens != 4 {
		t.Fatalf("tokens = %+v", entry.Tokens)
	}
}

func TestServer_ActivitySourceStripsClientPort(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{name: "IPv4", remoteAddr: "192.168.1.10:54321", want: "ip:192.168.1.10"},
		{name: "IPv6", remoteAddr: "[2001:db8::10]:54321", want: "ip:2001:db8::10"},
		{name: "missing port", remoteAddr: "localhost", want: "ip:localhost"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = test.remoteAddr
			r.Header.Set("X-Forwarded-For", "203.0.113.10:9999")
			if got := activitySource(r); got != test.want {
				t.Fatalf("activitySource() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestServer_ParseMetrics_Timings(t *testing.T) {
	body := `{"timings":{"prompt_n":20,"predicted_n":50,"prompt_per_second":100.0,"predicted_per_second":40.0,"prompt_ms":200,"predicted_ms":1250,"cache_n":8}}`
	parsed := gjson.Parse(body)
	entry, err := parseMetrics("m", time.Now(), parsed.Get("usage"), parsed.Get("timings"), parsed.Get("metrics"))
	if err != nil {
		t.Fatalf("parseMetrics: %v", err)
	}
	if entry.Tokens.InputTokens != 20 || entry.Tokens.OutputTokens != 50 || entry.Tokens.CachedTokens != 8 {
		t.Fatalf("tokens = %+v", entry.Tokens)
	}
	if entry.Tokens.TokensPerSecond != 40.0 || entry.Tokens.PromptPerSecond != 100.0 {
		t.Fatalf("rates = %+v", entry.Tokens)
	}
	if entry.DurationMs != 1450 {
		t.Fatalf("DurationMs = %d, want 1450", entry.DurationMs)
	}
}

func TestServer_ProcessStreamingResponse(t *testing.T) {
	body := []byte("data: {\"choices\":[{}]}\n\n" +
		"data: {\"usage\":{\"prompt_tokens\":15,\"completion_tokens\":33}}\n\n" +
		"data: [DONE]\n\n")
	entry, err := processStreamingResponse("m", time.Now(), body, nil)
	if err != nil {
		t.Fatalf("processStreamingResponse: %v", err)
	}
	if entry.Tokens.InputTokens != 15 || entry.Tokens.OutputTokens != 33 {
		t.Fatalf("tokens = %+v", entry.Tokens)
	}
}

func TestServer_ProcessStreamingResponse_VLLMMetrics(t *testing.T) {
	body := []byte(`data: {"id":"chatcmpl-b7a832cea986aea4","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":14,"total_tokens":166,"completion_tokens":152},"metrics":{"time_to_first_token_ms":70,"mean_itl_ms":10,"tokens_per_second":24.116032676555495}}

data: [DONE]
`)
	entry, err := processStreamingResponse("m", time.Now(), body, nil)
	if err != nil {
		t.Fatalf("processStreamingResponse: %v", err)
	}
	if entry.Tokens.InputTokens != 14 || entry.Tokens.OutputTokens != 152 {
		t.Fatalf("tokens = %+v", entry.Tokens)
	}
	if entry.Tokens.CachedTokens != -1 {
		t.Errorf("CachedTokens = %d, want -1", entry.Tokens.CachedTokens)
	}
	if got, want := entry.Tokens.PromptPerSecond, 200.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("PromptPerSecond = %v, want %v", got, want)
	}
	if entry.Tokens.TokensPerSecond != 100 {
		t.Errorf("TokensPerSecond = %v, want 100", entry.Tokens.TokensPerSecond)
	}
}

func TestServer_ParseMetrics_VLLMMetrics(t *testing.T) {
	body := `{"id":"chatcmpl-abc123","object":"chat.completion","usage":{"prompt_tokens":42,"completion_tokens":128,"total_tokens":170,"prompt_tokens_details":{"cached_tokens":20}},"metrics":{"time_to_first_token_ms":85.2,"generation_time_ms":1240.5,"queue_time_ms":12.3,"mean_itl_ms":9.1,"tokens_per_second":103.2}}`
	parsed := gjson.Parse(body)
	entry, err := parseMetrics("m", time.Now(), parsed.Get("usage"), parsed.Get("timings"), parsed.Get("metrics"))
	if err != nil {
		t.Fatalf("parseMetrics: %v", err)
	}
	if entry.Tokens.InputTokens != 42 || entry.Tokens.OutputTokens != 128 || entry.Tokens.CachedTokens != 20 {
		t.Fatalf("tokens = %+v", entry.Tokens)
	}
	if got, want := entry.Tokens.PromptPerSecond, float64(42-20)/(85.2/1000); math.Abs(got-want) > 1e-9 {
		t.Errorf("PromptPerSecond = %v, want %v", got, want)
	}
	if got, want := entry.Tokens.TokensPerSecond, 1000/9.1; math.Abs(got-want) > 1e-9 {
		t.Errorf("TokensPerSecond = %v, want %v", got, want)
	}
}

func TestServer_ParseMetrics_VLLMSpeculativeDecoding(t *testing.T) {
	body := `{"id":"chatcmpl-abc123","object":"chat.completion","usage":{"prompt_tokens":42,"completion_tokens":128},"metrics":{"mean_itl_ms":9.1,"speculative_decoding":{"mean_acceptance_length":1.7,"draft_acceptance_rate":0.7,"acceptance_histogram":[6,14],"num_spec_steps":20,"num_accepted_draft_tokens":14,"num_draft_tokens":20,"num_spec_tokens":1}}}`
	parsed := gjson.Parse(body)
	entry, err := parseMetrics("m", time.Now(), parsed.Get("usage"), parsed.Get("timings"), parsed.Get("metrics"))
	if err != nil {
		t.Fatalf("parseMetrics: %v", err)
	}
	if entry.Tokens.DraftTokens != 20 || entry.Tokens.DraftAccTokens != 14 {
		t.Fatalf("draft tokens = %+v, want 14/20", entry.Tokens)
	}
}

// A speculative_decoding object missing either counter must leave both unset so
// the acceptance rate is not computed from half the data.
func TestServer_ParseMetrics_VLLMSpeculativeDecodingPartial(t *testing.T) {
	body := `{"usage":{"prompt_tokens":42,"completion_tokens":128},"metrics":{"speculative_decoding":{"num_draft_tokens":20}}}`
	parsed := gjson.Parse(body)
	entry, err := parseMetrics("m", time.Now(), parsed.Get("usage"), parsed.Get("timings"), parsed.Get("metrics"))
	if err != nil {
		t.Fatalf("parseMetrics: %v", err)
	}
	if entry.Tokens.DraftTokens != -1 || entry.Tokens.DraftAccTokens != -1 {
		t.Fatalf("draft tokens = %+v, want -1/-1", entry.Tokens)
	}
}

// vLLM only emits the metrics object on the final streamed chunk.
func TestServer_ProcessStreamingResponse_VLLMSpeculativeDecoding(t *testing.T) {
	body := []byte(`data: {"choices":[{"delta":{"content":"hi"}}]}

data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":11742,"completion_tokens":35},"metrics":{"mean_itl_ms":17.209908293272534,"speculative_decoding":{"mean_acceptance_length":1.7,"draft_acceptance_rate":0.7,"num_spec_steps":20,"num_accepted_draft_tokens":14,"num_draft_tokens":20,"num_spec_tokens":1}}}

data: [DONE]
`)
	entry, err := processStreamingResponse("m", time.Now(), body, nil)
	if err != nil {
		t.Fatalf("processStreamingResponse: %v", err)
	}
	if entry.Tokens.DraftTokens != 20 || entry.Tokens.DraftAccTokens != 14 {
		t.Fatalf("draft tokens = %+v, want 14/20", entry.Tokens)
	}
}

// llama-server timings and vLLM metrics never appear together, but a response
// carrying both must not have its timings-sourced draft counts clobbered.
func TestServer_ParseMetrics_TimingsDraftTokensNotOverwritten(t *testing.T) {
	body := `{"timings":{"prompt_n":20,"predicted_n":50,"draft_n":30,"draft_n_accepted":12},"metrics":{"speculative_decoding":{}}}`
	parsed := gjson.Parse(body)
	entry, err := parseMetrics("m", time.Now(), parsed.Get("usage"), parsed.Get("timings"), parsed.Get("metrics"))
	if err != nil {
		t.Fatalf("parseMetrics: %v", err)
	}
	if entry.Tokens.DraftTokens != 30 || entry.Tokens.DraftAccTokens != 12 {
		t.Fatalf("draft tokens = %+v, want 12/30", entry.Tokens)
	}
}

func TestServer_ProcessStreamingResponse_NoData(t *testing.T) {
	if _, err := processStreamingResponse("m", time.Now(), []byte("data: [DONE]\n\n"), nil); err == nil {
		t.Fatal("expected error for stream with no usage data")
	}
}

func TestServer_ProcessStreamingResponse_ObservedRates(t *testing.T) {
	stream := func(final string) []byte {
		return []byte("data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"one\"}}]}\n\n" +
			"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"two\"}}]}\n\n" + final + "\n\ndata: [DONE]\n\n")
	}
	finalUsage := `data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2}}}`
	observed := &streamRateObserver{format: streamFormatChatCompletions, firstOutput: 200 * time.Millisecond, lastOutput: 700 * time.Millisecond}
	singleWrite := &streamRateObserver{format: streamFormatChatCompletions, firstOutput: 200 * time.Millisecond, lastOutput: 200 * time.Millisecond}

	tests := []struct {
		name       string
		body       []byte
		observer   *streamRateObserver
		wantPrompt float64
		wantOutput float64
	}{
		{name: "estimates both rates", body: stream(finalUsage), observer: observed, wantPrompt: 40, wantOutput: 8},
		{name: "disabled format", body: stream(finalUsage), observer: nil, wantPrompt: -1, wantOutput: -1},
		{name: "missing final usage", body: stream(`data: {"object":"chat.completion.chunk","choices":[{"delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`), observer: observed, wantPrompt: -1, wantOutput: -1},
		{name: "usage without standard object", body: stream(`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`), observer: observed, wantPrompt: -1, wantOutput: -1},
		{name: "one observed write", body: stream(finalUsage), observer: singleWrite, wantPrompt: 40, wantOutput: -1},
		{name: "unknown cache", body: stream(`data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`), observer: observed, wantPrompt: 50, wantOutput: 8},
		{name: "excessive cache", body: stream(`data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":12}}}`), observer: observed, wantPrompt: -1, wantOutput: 8},
		{name: "one output token", body: stream(`data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":1}}`), observer: observed, wantPrompt: 50, wantOutput: -1},
		{name: "zero output tokens", body: stream(`data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":0}}`), observer: observed, wantPrompt: 50, wantOutput: -1},
		{name: "native rates win", body: stream(`data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2}},"metrics":{"time_to_first_token_ms":100,"mean_itl_ms":20}}`), observer: observed, wantPrompt: 80, wantOutput: 50},
		{name: "partial native prompt", body: stream(`data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2}},"metrics":{"time_to_first_token_ms":100}}`), observer: observed, wantPrompt: 80, wantOutput: 8},
		{name: "partial native output", body: stream(`data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2}},"metrics":{"mean_itl_ms":20}}`), observer: observed, wantPrompt: 40, wantOutput: 50},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry, err := processStreamingResponse("m", time.Now(), test.body, test.observer)
			if err != nil {
				t.Fatalf("processStreamingResponse: %v", err)
			}
			if entry.Tokens.PromptPerSecond != test.wantPrompt || entry.Tokens.TokensPerSecond != test.wantOutput {
				t.Fatalf("rates = %v/%v, want %v/%v", entry.Tokens.PromptPerSecond, entry.Tokens.TokensPerSecond, test.wantPrompt, test.wantOutput)
			}
		})
	}
}

func TestServer_StreamFormatForPath(t *testing.T) {
	tests := []struct {
		path string
		want streamFormat
	}{
		{path: "/v1/chat/completions", want: streamFormatChatCompletions},
		{path: "/v/chat/completions", want: streamFormatChatCompletions},
		{path: "/v1/responses", want: streamFormatDisabled},
		{path: "/v1/chat/completions/", want: streamFormatDisabled},
	}

	for _, test := range tests {
		if got := streamFormatForPath(test.path); got != test.want {
			t.Errorf("streamFormatForPath(%q) = %d, want %d", test.path, got, test.want)
		}
	}
}

func TestMetricsMonitor_RecordObservedStreamRates(t *testing.T) {
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 0)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	recorder := httptest.NewRecorder()
	copier := newBodyCopier(recorder, streamFormatChatCompletions)
	copier.Header().Set("Content-Type", "text/event-stream")
	body := []byte("data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"one\"}}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"two\"}}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"prompt_tokens_details\":{\"cached_tokens\":2}}}\n\n")
	copier.Write(body)
	copier.observer.firstOutput = 200 * time.Millisecond
	copier.observer.lastOutput = 700 * time.Millisecond

	mm.record("m", r, copier, 0, nil, nil)

	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if got := entries[0].Tokens; got.PromptPerSecond != 40 || got.TokensPerSecond != 8 {
		t.Fatalf("rates = %v/%v, want 40/8", got.PromptPerSecond, got.TokensPerSecond)
	}
}

func TestMetricsMonitor_RecordCompressedStreamSkipsObservedRates(t *testing.T) {
	plain := []byte("data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"one\"}}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5},\"metrics\":{\"mean_itl_ms\":20}}\n\n")
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(plain); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 0)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	recorder := httptest.NewRecorder()
	copier := newBodyCopier(recorder, streamFormatChatCompletions)
	copier.Header().Set("Content-Type", "text/event-stream")
	copier.Header().Set("Content-Encoding", "gzip")
	copier.Write(compressed.Bytes())

	mm.record("m", r, copier, 0, nil, nil)

	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	got := entries[0].Tokens
	if got.InputTokens != 10 || got.OutputTokens != 5 {
		t.Fatalf("tokens = %+v, want input=10 output=5", got)
	}
	if got.PromptPerSecond != -1 || got.TokensPerSecond != 50 {
		t.Fatalf("rates = %v/%v, want unavailable prompt and native output 50", got.PromptPerSecond, got.TokensPerSecond)
	}
}

func TestServer_MetricsMiddleware_StreamRateFormats(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		requestBody  string
		stream       bool
		wantEstimate bool
	}{
		{name: "v1 chat", path: "/v1/chat/completions", requestBody: `{"model":"m"}`, stream: true, wantEstimate: true},
		{name: "v chat", path: "/v/chat/completions", requestBody: `{"model":"m"}`, stream: true, wantEstimate: true},
		{name: "upstream chat", path: "/upstream/m/v1/chat/completions", requestBody: `{}`, stream: true, wantEstimate: true},
		{name: "responses SSE", path: "/v1/responses", requestBody: `{"model":"m"}`, stream: true},
		{name: "non-streaming chat", path: "/v1/chat/completions", requestBody: `{"model":"m"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 0)
			cfg := config.Config{Models: map[string]config.ModelConfig{"m": {}}}
			handler := CreateMetricsMiddleware(mm, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !test.stream {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
					return
				}
				// Backdate the start so the first write has a non-zero elapsed time.
				w.(*responseBodyCopier).start = time.Now().Add(-time.Second)
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte("data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"one\"}}]}\n\n"))
				w.Write([]byte("data: {\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n"))
			}))

			r := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.requestBody))
			r.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(httptest.NewRecorder(), r)

			entries := metricsEntries(t, mm)
			if len(entries) != 1 {
				t.Fatalf("want 1 entry, got %d", len(entries))
			}
			gotEstimate := entries[0].Tokens.PromptPerSecond > 0
			if gotEstimate != test.wantEstimate {
				t.Fatalf("PromptPerSecond = %v, want estimate %t", entries[0].Tokens.PromptPerSecond, test.wantEstimate)
			}
		})
	}
}

func TestServer_ClassifyChatStreamEvent(t *testing.T) {
	tests := []struct {
		name string
		json string
		want streamEventFlags
	}{
		{name: "content", json: `{"object":"chat.completion.chunk","choices":[{"delta":{"content":"hello"}}]}`, want: streamEventOutput},
		{name: "reasoning content", json: `{"object":"chat.completion.chunk","choices":[{"delta":{"reasoning_content":"think"}}]}`, want: streamEventOutput},
		{name: "reasoning", json: `{"object":"chat.completion.chunk","choices":[{"delta":{"reasoning":"think"}}]}`, want: streamEventOutput},
		{name: "tool calls", json: `{"object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`, want: streamEventOutput},
		{name: "legacy function call", json: `{"object":"chat.completion.chunk","choices":[{"delta":{"function_call":{"arguments":"{}"}}}]}`, want: streamEventOutput},
		{name: "later choice has output", json: `{"object":"chat.completion.chunk","choices":[{"delta":{}},{"delta":{"content":"hello"}}]}`, want: streamEventOutput},
		{name: "final usage", json: `{"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`, want: streamEventComplete},
		{name: "continuous usage", json: `{"object":"chat.completion.chunk","choices":[{"delta":{}}],"usage":{"prompt_tokens":4}}`},
		{name: "loading reasoning has no object", json: `{"choices":[{"delta":{"reasoning_content":"Loading model"}}]}`},
		{name: "role only", json: `{"object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"}}]}`},
		{name: "finish only", json: `{"object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}]}`},
		{name: "extension only", json: `{"object":"chat.completion.chunk","choices":[{"delta":{}}],"kv_transfer_params":{"foo":"bar"}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyStreamEvent(streamFormatChatCompletions, gjson.Parse(test.json))
			if got != test.want {
				t.Fatalf("classifyStreamEvent() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestServer_StreamRateObserver(t *testing.T) {
	observer := newStreamRateObserver(streamFormatChatCompletions)
	observer.observe([]byte("data: not-json\n: comment\ndata: [DONE]\n"), 100*time.Millisecond)
	observer.observe([]byte(`data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"hel`), 150*time.Millisecond)
	observer.observe([]byte("lo"+`"}}]}`+"\n"), 200*time.Millisecond)
	observer.observe([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"Loading model\"}}]}\n"+
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n"), 500*time.Millisecond)
	observer.observe([]byte(`data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"unfinished"}}]}`), 900*time.Millisecond)

	if observer.firstOutput != 200*time.Millisecond || observer.lastOutput != 500*time.Millisecond {
		t.Fatalf("first/last = %v/%v, want 200ms/500ms", observer.firstOutput, observer.lastOutput)
	}
}

func TestServer_StreamRateObserver_BatchedEventsShareWriteTime(t *testing.T) {
	observer := newStreamRateObserver(streamFormatChatCompletions)
	observer.observe([]byte("data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"one\"}}]}\n"+
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"two\"}}]}\n"), 250*time.Millisecond)

	if observer.firstOutput != 250*time.Millisecond || observer.lastOutput != 250*time.Millisecond {
		t.Fatalf("first/last = %v/%v, want both 250ms", observer.firstOutput, observer.lastOutput)
	}
}

func TestMetricsMonitor_RecordMetadata(t *testing.T) {
	mm := newTestMetricsMonitor(t, nil, 10, 0)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"usage":{}}`))
	r = r.WithContext(swaputil.SetContext(r.Context(), swaputil.ReqContextData{
		ModelID:  "m",
		Metadata: map[string]string{"client": "web", "trace": "abc"},
	}))

	w := httptest.NewRecorder()
	copier := newBodyCopier(w, streamFormatDisabled)
	copier.WriteHeader(http.StatusOK)
	copier.Write([]byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2}}`))

	mm.record("m", r, copier, 0, nil, nil)

	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].Metadata["client"] != "web" {
		t.Errorf("client = %q, want web", entries[0].Metadata["client"])
	}
	if entries[0].Metadata["trace"] != "abc" {
		t.Errorf("trace = %q, want abc", entries[0].Metadata["trace"])
	}
}

// TestMetricsMonitor_RecordClientClosed covers #1029: a client that hangs up
// before a response is written must not be filed as a successful (empty-body)
// metric. It is recorded with the 499 sentinel and a client-cancelled
// ErrorMsg, and skips the capture the upstream-failure path would store.
func TestMetricsMonitor_RecordClientClosed(t *testing.T) {
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 5)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	w := httptest.NewRecorder()
	copier := newBodyCopier(w, streamFormatDisabled)
	// Nothing is ever written: this is the cold-load cancellation shape.
	copier.MarkStatus(swaputil.StatusClientClosedRequest)

	mm.record("m", r, copier, captureAll, []byte("req"), nil)

	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.RespStatusCode != swaputil.StatusClientClosedRequest {
		t.Errorf("status = %d, want %d", entry.RespStatusCode, swaputil.StatusClientClosedRequest)
	}
	if entry.ErrorMsg != "client disconnected before response" {
		t.Errorf("error_msg = %q, want client-cancelled message", entry.ErrorMsg)
	}
	if entry.HasCapture {
		t.Error("a cancelled request has no response to capture")
	}
}

// TestServer_MetricsMiddleware_ClientClosed checks the middleware derives the
// sentinel for a handler that returns without writing, and that the marker
// propagates outward to the access-log recorder so both agree.
func TestServer_MetricsMiddleware_ClientClosed(t *testing.T) {
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 0)
	cfg := config.Config{Models: map[string]config.ModelConfig{"m": {}}}

	proxylog := logmon.NewWriter(io.Discard)
	handler := chain.New(
		CreateRequestLogMiddleware(proxylog),
		CreateMetricsMiddleware(mm, cfg),
	).ThenFunc(func(w http.ResponseWriter, r *http.Request) {})

	ctx, cancel := context.WithCancel(context.Background())
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "192.168.1.1:5000"
	cancel()

	handler.ServeHTTP(httptest.NewRecorder(), r)

	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].RespStatusCode != swaputil.StatusClientClosedRequest {
		t.Errorf("activity status = %d, want %d", entries[0].RespStatusCode, swaputil.StatusClientClosedRequest)
	}
	if line := string(proxylog.GetHistory()); !strings.Contains(line, "499 0") {
		t.Errorf("access log %q should report 499", line)
	}
}

// TestServer_MetricsMiddleware_ServerSideCancelIsNotClientClosed guards the
// distinction #1029's fix depends on. The inflight middleware derives a
// cancellable context that POST /api/inflight/{id}/cancel fires, so testing
// "is this request's context done?" would report an operator cancelling a
// request from the UI as a client that hung up.
func TestServer_MetricsMiddleware_ServerSideCancelIsNotClientClosed(t *testing.T) {
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 0)
	cfg := config.Config{Models: map[string]config.ModelConfig{"m": {}}}

	proxylog := logmon.NewWriter(io.Discard)
	// The handler stands in for a dispatch that is cancelled mid-flight and
	// answers the still-connected client, as the proxy ErrorHandler does.
	handler := chain.New(
		CreateRequestLogMiddleware(proxylog),
		CreateMetricsMiddleware(mm, cfg),
	).ThenFunc(func(w http.ResponseWriter, r *http.Request) {
		derived, cancel := context.WithCancel(r.Context())
		defer cancel()
		cancel() // the operator cancels it
		r = r.WithContext(derived)
		if swaputil.MarkClientClosed(w, r) {
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	})

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "192.168.1.1:5000"

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("client got %d, want %d: a connected client must be answered", w.Code, http.StatusBadGateway)
	}
	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].RespStatusCode == swaputil.StatusClientClosedRequest {
		t.Error("a server-side cancel must not be recorded as a client disconnect")
	}
	if line := string(proxylog.GetHistory()); strings.Contains(line, "499") {
		t.Errorf("access log %q should not report 499 for a connected client", line)
	}
}

func TestMetricsMonitor_RecordFailedRequestCapture(t *testing.T) {
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 5)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	reqHeaders := map[string]string{"content-type": "application/json"}

	w := httptest.NewRecorder()
	copier := newBodyCopier(w, streamFormatDisabled)
	copier.Header().Set("Content-Type", "application/json")
	copier.WriteHeader(http.StatusBadGateway)
	copier.Write([]byte(`{"error":{"message":"model unavailable"}}`))

	reqBody := []byte(`{"model":"m","messages":[]}`)
	mm.record("m", r, copier, captureAll, reqBody, reqHeaders)

	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.RespStatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", entry.RespStatusCode, http.StatusBadGateway)
	}
	if entry.ErrorMsg != "model unavailable" {
		t.Errorf("error_msg = %q, want extracted message", entry.ErrorMsg)
	}
	if !entry.HasCapture {
		t.Fatal("failed request should capture the request so it can be inspected")
	}

	got := mm.getCaptureByID(entry.ID)
	if got == nil {
		t.Fatal("capture not found")
	}
	if string(got.ReqBody) != `{"model":"m","messages":[]}` {
		t.Errorf("req body = %q", got.ReqBody)
	}
	if len(got.RespBody) != 0 {
		t.Errorf("resp body stored for failed request (len=%d); want none", len(got.RespBody))
	}
	if got.RespHeaders["Content-Type"] != "application/json" {
		t.Errorf("resp Content-Type = %q", got.RespHeaders["Content-Type"])
	}
}

func TestMetricsMonitor_RecordFailedRequestStatusFallback(t *testing.T) {
	// Non-JSON error body: ErrorMsg falls back to the HTTP status text.
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 5)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	w := httptest.NewRecorder()
	copier := newBodyCopier(w, streamFormatDisabled)
	copier.WriteHeader(http.StatusBadGateway)
	copier.Write([]byte("<html>upstream down</html>"))

	mm.record("m", r, copier, captureAll, nil, nil)

	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].ErrorMsg != "502 Bad Gateway" {
		t.Errorf("error_msg = %q, want status text", entries[0].ErrorMsg)
	}
}

func TestMetricsMonitor_RecordFailedRequestCaptureDisabled(t *testing.T) {
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 0) // captures disabled
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	w := httptest.NewRecorder()
	copier := newBodyCopier(w, streamFormatDisabled)
	copier.WriteHeader(http.StatusInternalServerError)
	copier.Write([]byte(`{"error":"boom"}`))

	mm.record("m", r, copier, captureAll, []byte("req"), nil)

	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].HasCapture {
		t.Fatal("captures disabled, HasCapture should be false")
	}
	// ErrorMsg is independent of whether captures are enabled.
	if entries[0].ErrorMsg != "boom" {
		t.Errorf("error_msg = %q, want boom", entries[0].ErrorMsg)
	}
	if mm.getCaptureByID(entries[0].ID) != nil {
		t.Fatal("no capture should be stored when disabled")
	}
}

func TestMetricsMonitor_RecordDecompressionFailureSetsErrorMsg(t *testing.T) {
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 5)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	w := httptest.NewRecorder()
	copier := newBodyCopier(w, streamFormatDisabled)
	copier.Header().Set("Content-Encoding", "gzip")
	copier.WriteHeader(http.StatusOK)
	copier.Write([]byte("not-really-gzip"))

	mm.record("m", r, copier, captureAll, []byte("req"), nil)

	entries := metricsEntries(t, mm)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].ErrorMsg == "" {
		t.Fatal("expected ErrorMsg for decompression failure")
	}
	// Raw bytes must not be stored when the body could not be decoded.
	if entries[0].HasCapture {
		t.Fatal("decompression failure should not store a capture")
	}
}

func TestMetricsMonitor_DecodeResponseBody(t *testing.T) {
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 10, 5)

	// No Content-Encoding: body returned unchanged.
	w := httptest.NewRecorder()
	copier := newBodyCopier(w, streamFormatDisabled)
	copier.Write([]byte("plain"))
	got, err := mm.decodeResponseBody(copier, "/p")
	if err != nil || string(got) != "plain" {
		t.Fatalf("plain body = %q, err = %v", got, err)
	}

	// Bogus gzip payload: returns an error and no body (no raw bytes kept).
	w2 := httptest.NewRecorder()
	copier2 := newBodyCopier(w2, streamFormatDisabled)
	copier2.Header().Set("Content-Encoding", "gzip")
	copier2.Write([]byte("not-really-gzip"))
	got, err = mm.decodeResponseBody(copier2, "/p")
	if err == nil {
		t.Fatal("expected decompression error")
	}
	if got != nil {
		t.Errorf("expected nil body on failure, got %q", got)
	}
}

func TestServer_ExtractErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"openai object", `{"error":{"message":"rate limited"}}`, "rate limited"},
		{"string error", `{"error":"bad request"}`, "bad request"},
		{"message field", `{"message":"nope"}`, "nope"},
		{"detail field", `{"detail":"oops"}`, "oops"},
		{"object error ignored", `{"error":{"code":42}}`, ""},
		{"no error", `{"usage":{}}`, ""},
		{"invalid json", `not-json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractErrorMessage([]byte(tc.body)); got != tc.want {
				t.Errorf("extractErrorMessage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestServer_ParseMetrics_Infill(t *testing.T) {
	// /infill responses are arrays; timings live in the last element.
	body := `[{"content":"a"},{"content":"b","timings":{"prompt_n":5,"predicted_n":9,"prompt_ms":10,"predicted_ms":20}}]`
	parsed := gjson.Parse(body)
	timings := parsed.Get("timings")
	if arr := parsed.Array(); len(arr) > 0 {
		timings = arr[len(arr)-1].Get("timings")
	}
	entry, err := parseMetrics("m", time.Now(), parsed.Get("usage"), timings, parsed.Get("metrics"))
	if err != nil {
		t.Fatalf("parseMetrics: %v", err)
	}
	if entry.Tokens.InputTokens != 5 || entry.Tokens.OutputTokens != 9 {
		t.Fatalf("tokens = %+v", entry.Tokens)
	}
}

// TestServer_MetricsMiddleware_UpstreamAudioCaptureSkipsRespBody verifies that
// an /upstream/<model>/v1/audio/speech request uses the path-specific capture
// mask (headers only) rather than falling back to captureAll.
func TestServer_MetricsMiddleware_UpstreamAudioCaptureSkipsRespBody(t *testing.T) {
	mm := newTestMetricsMonitor(t, logmon.NewWriter(io.Discard), 100, 5)
	cfg := config.Config{Models: map[string]config.ModelConfig{"m1": {}}}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("BINARY-AUDIO-DATA"))
	})
	handler := CreateMetricsMiddleware(mm, cfg)(inner)

	req := httptest.NewRequest(http.MethodPost, "/upstream/m1/v1/audio/speech", strings.NewReader(`{"model":"m1"}`))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	entries := metricsEntries(t, mm)
	if len(entries) == 0 {
		t.Fatal("no metrics recorded")
	}
	last := entries[len(entries)-1]
	if !last.HasCapture {
		t.Fatal("expected capture to be stored")
	}
	cap := mm.getCaptureByID(last.ID)
	if cap == nil {
		t.Fatal("capture not found")
	}
	if len(cap.RespBody) != 0 {
		t.Errorf("RespBody stored for /upstream audio route (len=%d); want path-specific mask to skip body", len(cap.RespBody))
	}
	if len(cap.RespHeaders) == 0 {
		t.Error("RespHeaders not stored; want captureRespHeaders mask")
	}
}
