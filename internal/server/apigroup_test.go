package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServer_InflightMiddleware(t *testing.T) {
	c := &inflightCounter{}
	mw := CreateInflightMiddleware(c)

	var duringRequest int64
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		duringRequest = c.Current()
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	if duringRequest != 1 {
		t.Errorf("counter during request = %d, want 1", duringRequest)
	}
	if got := c.Current(); got != 0 {
		t.Errorf("counter after request = %d, want 0", got)
	}
}

func TestServer_APIVersion(t *testing.T) {
	s := newTestServer(newStubRouter(nil, ""), newStubRouter(nil, ""))
	s.build = BuildInfo{Version: "1.2.3", Commit: "deadbeef", Date: "2026-05-19"}

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["version"] != "1.2.3" || got["commit"] != "deadbeef" || got["build_date"] != "2026-05-19" {
		t.Errorf("body = %v", got)
	}
	if got["pin_required"] != false {
		t.Errorf("pin_required = %v, want false", got["pin_required"])
	}
}

func TestServer_APIVersion_PinRequired(t *testing.T) {
	s := newTestServer(newStubRouter(nil, ""), newStubRouter(nil, ""))
	s.cfg.AdminPin = "1234"

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["pin_required"] != true {
		t.Errorf("pin_required = %v, want true", got["pin_required"])
	}
}

func TestServer_VerifyPin(t *testing.T) {
	verify := func(t *testing.T, s *Server, body string) (int, bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/verify-pin", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		var resp struct {
			OK bool `json:"ok"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp.OK
	}

	t.Run("no pin configured always ok", func(t *testing.T) {
		s := newTestServer(newStubRouter(nil, ""), newStubRouter(nil, ""))
		code, ok := verify(t, s, `{"pin":"anything"}`)
		if code != http.StatusOK || !ok {
			t.Errorf("code=%d ok=%v, want 200 true", code, ok)
		}
	})

	t.Run("correct pin", func(t *testing.T) {
		s := newTestServer(newStubRouter(nil, ""), newStubRouter(nil, ""))
		s.cfg.AdminPin = "1234"
		code, ok := verify(t, s, `{"pin":"1234"}`)
		if code != http.StatusOK || !ok {
			t.Errorf("code=%d ok=%v, want 200 true", code, ok)
		}
	})

	t.Run("wrong pin", func(t *testing.T) {
		s := newTestServer(newStubRouter(nil, ""), newStubRouter(nil, ""))
		s.cfg.AdminPin = "1234"
		code, ok := verify(t, s, `{"pin":"0000"}`)
		if code != http.StatusUnauthorized || ok {
			t.Errorf("code=%d ok=%v, want 401 false", code, ok)
		}
	})

	t.Run("bad body", func(t *testing.T) {
		s := newTestServer(newStubRouter(nil, ""), newStubRouter(nil, ""))
		s.cfg.AdminPin = "1234"
		req := httptest.NewRequest(http.MethodPost, "/api/verify-pin", strings.NewReader("not json"))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("code=%d, want 400", w.Code)
		}
	})
}

func TestServer_APIMetrics_Empty(t *testing.T) {
	s := newTestServer(newStubRouter(nil, ""), newStubRouter(nil, ""))

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if body := strings.TrimSpace(w.Body.String()); body != "[]" {
		t.Errorf("body = %q, want []", body)
	}
}

func TestServer_APIPerformance_Unavailable(t *testing.T) {
	s := newTestServer(newStubRouter(nil, ""), newStubRouter(nil, ""))

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/performance", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestServer_APIEvents_InitialPayload(t *testing.T) {
	s := newTestServer(newStubRouter(nil, ""), newStubRouter(nil, ""))

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		s.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after context cancel")
	}

	body := w.Body.String()
	for _, want := range []string{`"type":"modelStatus"`, `"type":"inflight"`, `"type":"logData"`} {
		if !strings.Contains(body, want) {
			t.Errorf("initial SSE payload missing %s; body=%q", want, body)
		}
	}
}
