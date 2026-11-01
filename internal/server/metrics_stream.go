package server

import (
	"bytes"
	"time"

	"github.com/tidwall/gjson"
)

// chatStreamObserver observes SSE output arrival for streamed Chat Completions
// so Activity can fall back to a proxy-measured decode rate when an upstream
// reports token counts without timing fields. Prefill rate is never estimated
// because request-to-first-token time includes queueing and model loading.
type chatStreamObserver struct {
	partialLine []byte
	lastWrite   time.Duration
	firstOutput time.Duration
	lastOutput  time.Duration
	finalOutput int64
}

func newChatStreamObserver(path string) *chatStreamObserver {
	switch path {
	case "/v1/chat/completions", "/v/chat/completions":
		return &chatStreamObserver{firstOutput: -1, lastOutput: -1, finalOutput: -1}
	default:
		return nil
	}
}

func (o *chatStreamObserver) observe(data []byte, elapsed time.Duration) {
	o.lastWrite = elapsed
	for len(data) > 0 {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			o.partialLine = append(o.partialLine, data...)
			return
		}
		line := data[:nl]
		if len(o.partialLine) > 0 {
			o.partialLine = append(o.partialLine, line...)
			line = o.partialLine
		}
		o.observeLine(line, elapsed)
		o.partialLine = o.partialLine[:0]
		data = data[nl+1:]
	}
}

func (o *chatStreamObserver) observeLine(line []byte, elapsed time.Duration) {
	line = bytes.TrimSpace(line)
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	data := bytes.TrimSpace(line[len("data:"):])
	if !gjson.ValidBytes(data) {
		return
	}
	ev := gjson.ParseBytes(data)
	// Loading status frames reuse the delta shape without this object type.
	if ev.Get("object").String() != "chat.completion.chunk" {
		return
	}
	choices := ev.Get("choices")
	if choices.IsArray() && len(choices.Array()) == 0 && ev.Get("usage").IsObject() {
		output := ev.Get("usage.completion_tokens")
		o.finalOutput = -1
		if output.Type == gjson.Number && output.Int() >= 0 && output.Float() == float64(output.Int()) {
			o.finalOutput = output.Int()
		}
	}
	if !chatStreamHasOutput(ev) {
		return
	}
	if o.firstOutput < 0 {
		o.firstOutput = elapsed
	}
	o.lastOutput = elapsed
}

func chatStreamHasOutput(ev gjson.Result) bool {
	for _, choice := range ev.Get("choices").Array() {
		delta := choice.Get("delta")
		if delta.Get("content").String() != "" ||
			delta.Get("reasoning_content").String() != "" ||
			delta.Get("reasoning").String() != "" ||
			len(delta.Get("tool_calls").Array()) > 0 ||
			len(delta.Get("function_call").Map()) > 0 {
			return true
		}
	}
	return false
}

// applyRateFallback fills an unknown decode rate after native extraction using
// the first-to-last output interval, excluding model loading, queueing, and
// prefill. A final usage chunk is required, even if earlier chunks carried
// partial counts. Reported rates (including zero) and PromptPerSecond are
// never modified.
func (o *chatStreamObserver) applyRateFallback(tokens *TokenMetrics) {
	if o == nil {
		return
	}
	if len(o.partialLine) > 0 {
		o.observeLine(o.partialLine, o.lastWrite)
		o.partialLine = nil
	}
	if o.firstOutput < 0 || o.finalOutput < 2 || tokens.TokensPerSecond != -1 {
		return
	}
	if tokens.OutputTokens >= 2 && o.lastOutput > o.firstOutput {
		tokens.TokensPerSecond = float64(tokens.OutputTokens-1) / (o.lastOutput - o.firstOutput).Seconds()
	}
}
