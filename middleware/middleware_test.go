package middleware

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnvVarChecker(t *testing.T) {
	const present = "ROUTER_GO_PRESENT"
	t.Setenv(present, "yes")

	presentCalls := 0
	presentHandler := EnvVarChecker(present)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		presentCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	presentRec := httptest.NewRecorder()
	presentHandler.ServeHTTP(presentRec, httptest.NewRequest(http.MethodGet, "/", nil))

	if presentRec.Code != http.StatusNoContent || presentCalls != 1 {
		t.Fatalf("present variable: status=%d calls=%d", presentRec.Code, presentCalls)
	}

	const missing = "ROUTER_GO_MISSING"
	t.Setenv(missing, "")
	missingCalls := 0
	missingHandler := EnvVarChecker(missing)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		missingCalls++
		_, _ = io.WriteString(w, "|next")
	}))
	missingRec := httptest.NewRecorder()
	missingHandler.ServeHTTP(missingRec, httptest.NewRequest(http.MethodGet, "/", nil))

	if missingRec.Code != http.StatusInternalServerError {
		t.Fatalf("missing variable status = %d, want %d", missingRec.Code, http.StatusInternalServerError)
	}
	if missingCalls != 1 {
		t.Fatalf("next calls = %d, want 1", missingCalls)
	}
	wantBody := "Missing required environment variables: [ROUTER_GO_MISSING]|next"
	if got := missingRec.Body.String(); got != wantBody {
		t.Fatalf("body = %q, want %q", got, wantBody)
	}
}

func TestLoggerWithConfig(t *testing.T) {
	var output bytes.Buffer
	handler := LoggerWithConfig(LoggerConfig{
		IncludeTimestamp: false,
		Output:           &output,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/items", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	logLine := output.String()
	for _, part := range []string{"POST", "/items", "192.0.2.1:1234", "201", " in "} {
		if !strings.Contains(logLine, part) {
			t.Fatalf("log output %q does not contain %q", logLine, part)
		}
	}
}

func TestLoggerDefault(t *testing.T) {
	handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestLoggerColorHelpers(t *testing.T) {
	statusTests := []struct {
		status int
		want   string
	}{
		{status: http.StatusOK, want: "\033[32m"},
		{status: http.StatusFound, want: "\033[36m"},
		{status: http.StatusNotFound, want: "\033[33m"},
		{status: http.StatusInternalServerError, want: "\033[31m"},
		{status: 0, want: "\033[0m"},
	}
	for _, tt := range statusTests {
		if got := getStatusColor(tt.status); got != tt.want {
			t.Errorf("getStatusColor(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}

	methodTests := map[string]string{
		http.MethodGet:     "\033[34m",
		http.MethodPost:    "\033[36m",
		http.MethodPut:     "\033[33m",
		http.MethodPatch:   "\033[35m",
		http.MethodDelete:  "\033[31m",
		http.MethodHead:    "\033[32m",
		http.MethodOptions: "\033[91m",
		http.MethodConnect: "\033[95m",
		http.MethodTrace:   "\033[96m",
		"OTHER":            "\033[0m",
	}
	for method, want := range methodTests {
		if got := getMethodColor(method); got != want {
			t.Errorf("getMethodColor(%q) = %q, want %q", method, got, want)
		}
	}

	durationTests := []struct {
		duration time.Duration
		want     string
	}{
		{duration: time.Millisecond, want: "\033[32m"},
		{duration: 200 * time.Millisecond, want: "\033[33m"},
		{duration: time.Second, want: "\033[31m"},
	}
	for _, tt := range durationTests {
		if got := getDurationColor(tt.duration); got != tt.want {
			t.Errorf("getDurationColor(%s) = %q, want %q", tt.duration, got, tt.want)
		}
	}
}

func TestRecoverer(t *testing.T) {
	normal := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	normalRec := httptest.NewRecorder()
	normal.ServeHTTP(normalRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if normalRec.Code != http.StatusAccepted {
		t.Fatalf("normal status = %d, want %d", normalRec.Code, http.StatusAccepted)
	}

	stderr, err := os.CreateTemp(t.TempDir(), "recoverer")
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = stderr.Close()
	})

	panicking := Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	panicRec := httptest.NewRecorder()
	panicking.ServeHTTP(panicRec, httptest.NewRequest(http.MethodGet, "/", nil))

	if panicRec.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d, want %d", panicRec.Code, http.StatusInternalServerError)
	}
	if got, want := panicRec.Body.String(), "Internal Server Error\n"; got != want {
		t.Fatalf("panic body = %q, want %q", got, want)
	}

	stack := formatStack([]byte("goroutine 1\n/path/file.go:10\nfunction"))
	if !strings.Contains(stack, Cyan+"  /path/file.go:10"+Reset) {
		t.Fatalf("formatted stack = %q", stack)
	}
	if !strings.Contains(stack, Yellow+"function") {
		t.Fatalf("formatted stack = %q", stack)
	}
}

func TestResponseWriterWrapper(t *testing.T) {
	base := httptest.NewRecorder()
	wrapped := &ResponseWriterWrapper{ResponseWriter: base, StatusCode: http.StatusOK}
	wrapped.WriteHeader(http.StatusCreated)

	if wrapped.StatusCode != http.StatusCreated {
		t.Fatalf("StatusCode = %d, want %d", wrapped.StatusCode, http.StatusCreated)
	}
	if base.Code != http.StatusCreated {
		t.Fatalf("underlying status = %d, want %d", base.Code, http.StatusCreated)
	}

	if conn, _, err := wrapped.Hijack(); err == nil || conn != nil {
		t.Fatalf("Hijack() = (%v, %v), want nil connection and error", conn, err)
	}
	wrapped.Flush()

	hijacker := newHijackWriter()
	defer hijacker.close()
	delegating := &ResponseWriterWrapper{ResponseWriter: hijacker}
	conn, _, err := delegating.Hijack()
	if err != nil {
		t.Fatalf("Hijack returned error: %v", err)
	}
	if conn != hijacker.server {
		t.Fatal("Hijack returned an unexpected connection")
	}
	delegating.Flush()
	if !hijacker.flushed {
		t.Fatal("Flush was not delegated")
	}
}

type hijackWriter struct {
	header  http.Header
	server  net.Conn
	client  net.Conn
	flushed bool
}

func newHijackWriter() *hijackWriter {
	server, client := net.Pipe()
	return &hijackWriter{
		header: http.Header{},
		server: server,
		client: client,
	}
}

func (w *hijackWriter) Header() http.Header {
	return w.header
}

func (w *hijackWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *hijackWriter) WriteHeader(int) {}

func (w *hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.server, bufio.NewReadWriter(bufio.NewReader(w.server), bufio.NewWriter(w.server)), nil
}

func (w *hijackWriter) Flush() {
	w.flushed = true
}

func (w *hijackWriter) close() {
	_ = w.server.Close()
	_ = w.client.Close()
}

func TestRateLimiter(t *testing.T) {
	calls := 0
	handler := RateLimiter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))

	first := httptest.NewRequest(http.MethodGet, "/", nil)
	first.RemoteAddr = "192.0.2.1:1234"
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d", firstRec.Code)
	}

	second := httptest.NewRequest(http.MethodGet, "/", nil)
	second.RemoteAddr = first.RemoteAddr
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusTooManyRequests)
	}
	if got, want := secondRec.Body.String(), "Too many requests\n"; got != want {
		t.Fatalf("second body = %q, want %q", got, want)
	}

	other := httptest.NewRequest(http.MethodGet, "/", nil)
	other.RemoteAddr = "192.0.2.2:1234"
	otherRec := httptest.NewRecorder()
	handler.ServeHTTP(otherRec, other)
	if otherRec.Code != http.StatusNoContent {
		t.Fatalf("other status = %d", otherRec.Code)
	}
	if calls != 2 {
		t.Fatalf("next calls = %d, want 2", calls)
	}
}

func TestThrottleLimitsConcurrency(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{}, 3)
	release := make(chan struct{})

	handler := Throttle(2)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
	}))

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}()
	}

	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for requests to enter")
		}
	}

	select {
	case <-entered:
		t.Fatal("third request entered before capacity was released")
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	wg.Wait()

	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", got)
	}
}

func TestAPIRateLimiter(t *testing.T) {
	limiter := NewAPIRateLimiter(2, 20*time.Millisecond)
	if !limiter.Allow() {
		t.Fatal("initial tokens were not available")
	}
	if !limiter.Allow() {
		t.Fatal("initial tokens were not available")
	}
	if limiter.Allow() {
		t.Fatal("limiter allowed a request without a token")
	}

	time.Sleep(25 * time.Millisecond)
	if !limiter.Allow() {
		t.Fatal("limiter did not refill a token")
	}

	waiting := NewAPIRateLimiter(1, 15*time.Millisecond)
	if !waiting.Allow() {
		t.Fatal("initial waiting token was not available")
	}
	start := time.Now()
	waiting.Wait()
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Fatalf("Wait returned too soon: %s", elapsed)
	}
}

func TestSingleFlight(t *testing.T) {
	group := NewSingleFlight()
	start := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	wantErr := errors.New("result")

	const goroutines = 8
	type result struct {
		value string
		err   error
	}
	results := make(chan result, goroutines)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			value, err := group.Do("key", func() ([]byte, error) {
				calls.Add(1)
				<-release
				return []byte("value"), wantErr
			})
			results <- result{value: string(value), err: err}
		}()
	}

	close(start)
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)

	if got := calls.Load(); got != 1 {
		t.Fatalf("function calls = %d, want 1", got)
	}
	for result := range results {
		if result.value != "value" || !errors.Is(result.err, wantErr) {
			t.Fatalf("result = (%q, %v)", result.value, result.err)
		}
	}

	_, _ = group.Do("key", func() ([]byte, error) {
		calls.Add(1)
		return nil, nil
	})
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls after completed invocation = %d, want 2", got)
	}
}

func TestSharedHTTPClientConfiguration(t *testing.T) {
	if SharedHTTPClient.Timeout != 10*time.Second {
		t.Fatalf("timeout = %s, want 10s", SharedHTTPClient.Timeout)
	}
	transport, ok := SharedHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", SharedHTTPClient.Transport)
	}
	if transport.MaxIdleConns != 100 ||
		transport.MaxIdleConnsPerHost != 10 ||
		transport.IdleConnTimeout != 90*time.Second ||
		transport.DisableKeepAlives {
		t.Fatalf("unexpected transport configuration: %+v", transport)
	}
}
