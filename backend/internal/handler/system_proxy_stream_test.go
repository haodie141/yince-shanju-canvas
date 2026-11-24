package handler

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSystemProxyEventStreamFlushesBeforeUpstreamCompletes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: second\n\n")
	}))
	defer upstream.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(upstream.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		context, _ := gin.CreateTestContext(w)
		context.Request = r
		_, _ = streamSystemProxyResponse(context, resp, 1<<20)
	}))
	defer proxy.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(proxy.URL)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	if response.Header.Get("X-Accel-Buffering") != "no" {
		close(release)
		_ = response.Body.Close()
		t.Fatalf("X-Accel-Buffering = %q", response.Header.Get("X-Accel-Buffering"))
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil {
		close(release)
		_ = response.Body.Close()
		t.Fatal(err)
	}
	if line != "data: first\n" {
		close(release)
		_ = response.Body.Close()
		t.Fatalf("first streamed line = %q", line)
	}
	close(release)
	_ = response.Body.Close()
}

type canceledAfterStream struct{ parts []string }

func (r *canceledAfterStream) Read(p []byte) (int, error) {
	if len(r.parts) == 0 {
		return 0, context.Canceled
	}
	part := r.parts[0]
	r.parts = r.parts[1:]
	return copy(p, part), nil
}

func (*canceledAfterStream) Close() error { return nil }

func TestSystemProxyEventStreamCompletedBeforeClientDisconnect(t *testing.T) {
	for _, tc := range []struct {
		name      string
		parts     []string
		wantError bool
	}{
		{"complete event split across reads", []string{"data: result\n\ndata: [DO", "NE]\n\n"}, false},
		{"complete CRLF event", []string{"data: result\r\n\r\ndata: [DONE]\r\n\r\n"}, false},
		{"incomplete event", []string{"data: result\n\ndata: [DONE]\n"}, true},
		{"disconnect during output", []string{"data: partial\n\n"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			upstream := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: &canceledAfterStream{parts: tc.parts}}
			_, err := streamSystemProxyResponse(c, upstream, 1<<20)
			if tc.wantError != errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want canceled = %v", err, tc.wantError)
			}
		})
	}
}

func TestSystemProxyEventStreamEnforcesResponseLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/ai/system/channel/responses", nil)
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: too large\n\n")),
	}

	captured, err := streamSystemProxyResponse(context, upstream, 4)
	if !errors.Is(err, errSystemProxyResponseTooLarge) {
		t.Fatalf("error = %v", err)
	}
	if len(captured) != 0 || response.Body.Len() != 0 {
		t.Fatalf("captured = %q, response = %q", captured, response.Body.String())
	}
}
