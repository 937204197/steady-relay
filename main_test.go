package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPinnedDialAddressIsStrictAndPreservesPort(t *testing.T) {
	tests := []struct {
		name    string
		address string
		host    string
		ip      string
		want    string
	}{
		{"exact", "api.example.test:443", "api.example.test", "127.0.0.1", "127.0.0.1:443"},
		{"case and trailing dot", "API.EXAMPLE.TEST.:8443", "api.example.test", "127.0.0.1", "127.0.0.1:8443"},
		{"IPv6 pin", "api.example.test:443", "api.example.test", "2001:db8::1", "[2001:db8::1]:443"},
		{"unrelated", "other.example.test:443", "api.example.test", "127.0.0.1", "other.example.test:443"},
		{"suffix is not exact", "x.api.example.test:443", "api.example.test", "127.0.0.1", "x.api.example.test:443"},
		{"disabled", "api.example.test:443", "api.example.test", "", "api.example.test:443"},
		{"malformed", "api.example.test", "api.example.test", "127.0.0.1", "api.example.test"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pinnedDialAddress(test.address, test.host, test.ip); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestPinnedHTTPSPreservesHostSNIAndCertificateVerification(t *testing.T) {
	const hostname = "upstream.pin.test"
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: hostname},
		DNSNames:     []string{hostname},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	sniSeen := make(chan string, 1)
	tlsListener := tls.NewListener(listener, &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: certificate}},
		MinVersion:   tls.VersionTLS12,
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sniSeen <- hello.ServerName
			return nil, nil
		},
	})
	hostSeen := make(chan string, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostSeen <- r.Host
		w.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = server.Serve(tlsListener) }()
	defer server.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := url.Parse("https://" + net.JoinHostPort(hostname, port) + "/v1")
	if err != nil {
		t.Fatal(err)
	}
	client := newUpstreamClient(config{upstream: upstream, upstreamIP: "127.0.0.1", requestLimit: 2 * time.Second})
	transport := client.Transport.(*http.Transport)
	transport.Proxy = nil
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}

	resp, err := client.Get(upstream.String())
	if err != nil {
		t.Fatalf("pinned HTTPS request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := <-sniSeen; got != hostname {
		t.Fatalf("SNI=%q, want %q", got, hostname)
	}
	if got := <-hostSeen; got != net.JoinHostPort(hostname, port) {
		t.Fatalf("Host=%q, want %q", got, net.JoinHostPort(hostname, port))
	}
}

func testProxy(t *testing.T, upstream *httptest.Server, retries int) *httptest.Server {
	t.Helper()
	u, err := url.Parse(upstream.URL + "/v1")
	if err != nil {
		t.Fatal(err)
	}
	p := &proxy{
		cfg:    config{upstream: u, maxRetries: retries, backoff: 0, maxRetryWait: time.Second},
		client: upstream.Client(),
	}
	return httptest.NewServer(p)
}

func TestTransparentBodyHeadersAndURL(t *testing.T) {
	payload := []byte{0, 1, 2, 254, 255}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if !bytes.Equal(got, payload) {
			t.Errorf("body changed: %v", got)
		}
		if r.URL.RequestURI() != "/v1/responses?x=a%2Fb" {
			t.Errorf("unexpected URI: %s", r.URL.RequestURI())
		}
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-Custom") != "untouched" {
			t.Errorf("end-to-end headers changed: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()

	server := testProxy(t, upstream, 0)
	defer server.Close()
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/responses?x=a%2Fb", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Custom", "untouched")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated || resp.Header.Get("X-Upstream") != "yes" || !bytes.Equal(got, payload) {
		t.Fatalf("response changed: status=%d headers=%v body=%v", resp.StatusCode, resp.Header, got)
	}
}

func TestListenWithFallbackUsesNextAvailablePort(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	host, portText, err := net.SplitHostPort(occupied.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port >= 65535 {
		t.Skip("OS selected an unsuitable test port")
	}
	listener, actual, err := listenWithFallback(net.JoinHostPort(host, portText))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, actualPortText, err := net.SplitHostPort(actual)
	if err != nil {
		t.Fatal(err)
	}
	actualPort, err := strconv.Atoi(actualPortText)
	if err != nil || actualPort <= port {
		t.Fatalf("expected a later port than %d, got %q", port, actual)
	}
}

func TestRequestedModelFromBody(t *testing.T) {
	if got := requestedModelFromBody([]byte(`{"model":"gpt-5.5","input":[]}`)); got != "gpt-5.5" {
		t.Fatalf("model=%q", got)
	}
	if got := requestedModelFromBody([]byte(`{"input":[]}`)); got != "" {
		t.Fatalf("missing model=%q", got)
	}
}

func TestRetryableStatusThenSuccess(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "overloaded", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 1)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if attempts.Load() != 2 || resp.StatusCode != 200 || string(body) != "ok" {
		t.Fatalf("attempts=%d status=%d body=%q", attempts.Load(), resp.StatusCode, body)
	}
}

func TestFinalRetryableResponseIsForwarded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Error", "upstream")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("exact error"))
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 0)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 429 || resp.Header.Get("X-Error") != "upstream" || string(body) != "exact error" {
		t.Fatalf("status=%d header=%q body=%q", resp.StatusCode, resp.Header.Get("X-Error"), body)
	}
}

func TestRetryExhaustionForwardsLastHTTPErrorExactly(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Attempt", strconv.Itoa(int(attempt)))
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"failure-%d"}}`, attempt)
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 1)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if attempts.Load() != 2 || resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("X-Upstream-Attempt") != "2" || string(body) != `{"error":{"message":"failure-2"}}` {
		t.Fatalf("attempts=%d status=%d headers=%v body=%q", attempts.Load(), resp.StatusCode, resp.Header, body)
	}
}

func TestUsageLimitReachedRetriesThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	exactBody := `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, exactBody)
			return
		}
		_, _ = io.WriteString(w, "ok-from-next-channel")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 1)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if attempts.Load() != 2 || resp.StatusCode != http.StatusOK || string(body) != "ok-from-next-channel" {
		t.Fatalf("attempts=%d status=%d body=%q", attempts.Load(), resp.StatusCode, body)
	}
}

func TestUsageLimitRetryExhaustionForwardsLastErrorExactly(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Attempt", strconv.Itoa(int(attempt)))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprintf(w, `{"error":{"type":"usage_limit_reached","message":"limit-%d"}}`, attempt)
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 1)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if attempts.Load() != 2 || resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("X-Upstream-Attempt") != "2" || string(body) != `{"error":{"type":"usage_limit_reached","message":"limit-2"}}` {
		t.Fatalf("attempts=%d status=%d headers=%v body=%q", attempts.Load(), resp.StatusCode, resp.Header, body)
	}
}

func TestSSEIsFlushed(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: second\n\n")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 0)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, len("data: first\n\n"))
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != "data: first\n\n" {
		t.Fatalf("first SSE event was not flushed: %q, %v", buf, err)
	}
	close(release)
	rest, _ := io.ReadAll(resp.Body)
	if string(rest) != "data: second\n\n" {
		t.Fatalf("second SSE event changed: %q", rest)
	}
}

func TestHealthAndPathGuard(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("health request reached upstream")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 0)
	defer server.Close()

	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("health: status=%d body=%q", resp.StatusCode, body)
	}
	resp, err = http.Get(server.URL + "/not-v1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("path guard returned %d", resp.StatusCode)
	}
}

func TestRetryAfter(t *testing.T) {
	if got, ok := parseRetryAfter("2", time.Now()); !ok || got != 2*time.Second {
		t.Fatalf("delta Retry-After: %v, %v", got, ok)
	}
	now := time.Now().Truncate(time.Second)
	header := now.Add(3 * time.Second).UTC().Format(http.TimeFormat)
	if got, ok := parseRetryAfter(header, now); !ok || got != 3*time.Second {
		t.Fatalf("date Retry-After: %v, %v (%s)", got, ok, fmt.Sprint(header))
	}
}

// captureProxyLogs keeps the assertions below focused on the diagnostic
// fields, while restoring the process-wide logger for the other tests.
func captureProxyLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	oldWriter, oldFlags, oldPrefix := log.Writer(), log.Flags(), log.Prefix()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})
	return &buf
}

func TestRetryDiagnosticsIncludeStatusAndRequestID(t *testing.T) {
	logs := captureProxyLogs(t)
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.Header().Set("X-Request-Id", "upstream-req-123")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("X-Request-Id", "upstream-ok-456")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 1)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || attempts.Load() != 2 {
		t.Fatalf("attempts=%d status=%d", attempts.Load(), resp.StatusCode)
	}
	got := logs.String()
	for _, want := range []string{
		"[request] id=req-",
		"method=GET path=/v1/responses model=\"\" forwarding",
		"[retry] id=req-",
		"upstream_status=429",
		"upstream_request_id=\"upstream-req-123\"",
		"[response] id=req-",
		"status=200 attempts=2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostic log missing %q in %q", want, got)
		}
	}
}

func TestSSEDiagnosticsReportCompleted(t *testing.T) {
	logs := captureProxyLogs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-Id", "sse-complete-1")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 0)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "response.completed") {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	got := logs.String()
	for _, want := range []string{
		"stream=complete",
		"terminal=\"response.completed\"",
		"events=1",
		"upstream_request_id=\"sse-complete-1\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostic log missing %q in %q", want, got)
		}
	}
}

func TestSSEDiagnosticsReportResponseFailed(t *testing.T) {
	logs := captureProxyLogs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"server_error\",\"code\":\"capacity\",\"message\":\"Selected model is at capacity\"}}}\n\n")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 0)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Selected model is at capacity") || !strings.Contains(string(body), "response.failed") {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	got := logs.String()
	for _, want := range []string{
		"terminal=\"response.failed\"",
		"upstream_error_class=\"model_capacity\"",
		"upstream_error_type=\"server_error\"",
		"upstream_error_code=\"capacity\"",
		"upstream_error_message=\"Selected model is at capacity\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostic log missing %q in %q", want, got)
		}
	}
}

func TestSSEFinalFailureForwardsOnlyLastUpstreamEvent(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: response.metadata\ndata: {\"type\":\"response.metadata\",\"attempt\":%d}\n\n", attempt)
		_, _ = fmt.Fprintf(w, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"overloaded-attempt-%d\"}}}\n\n", attempt)
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 1)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if attempts.Load() != 2 || resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") || strings.Contains(string(body), "overloaded-attempt-1") || !strings.Contains(string(body), "overloaded-attempt-2") {
		t.Fatalf("attempts=%d status=%d body=%q", attempts.Load(), resp.StatusCode, body)
	}
}

func TestSSECapacityBeforeCommitRetriesWithoutForwardingFailure(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\"}\n\n")
			_, _ = io.WriteString(w, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"capacity\"}}}\n\n")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\"}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 1)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if attempts.Load() != 2 || resp.StatusCode != http.StatusOK || strings.Contains(string(body), "response.failed") || !strings.Contains(string(body), "response.output_text.delta") {
		t.Fatalf("attempts=%d status=%d body=%q", attempts.Load(), resp.StatusCode, body)
	}
}

func TestSSEUsageLimitBeforeCommitRetriesWithoutWaitingForEOF(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"usage_limit_reached\",\"message\":\"channel exhausted\"}}}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok-from-next-channel\"}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 1)
	defer server.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if attempts.Load() != 2 || resp.StatusCode != http.StatusOK || strings.Contains(string(body), "channel exhausted") || !strings.Contains(string(body), "ok-from-next-channel") {
		t.Fatalf("attempts=%d status=%d body=%q", attempts.Load(), resp.StatusCode, body)
	}
}

func TestSSEUsageLimitAfterOutputDoesNotRetry(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"already-sent\"}\n\n")
		_, _ = io.WriteString(w, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"usage_limit_reached\",\"message\":\"channel exhausted\"}}}\n\n")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 5)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if attempts.Load() != 1 || !strings.Contains(string(body), "already-sent") || !strings.Contains(string(body), "channel exhausted") {
		t.Fatalf("attempts=%d status=%d body=%q", attempts.Load(), resp.StatusCode, body)
	}
}

func TestSSEMetadataBeforeCapacityFailureRetries(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			_, _ = io.WriteString(w, "event: response.metadata\ndata: {\"type\":\"response.metadata\",\"metadata\":{}}\n\n")
			_, _ = io.WriteString(w, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"overloaded\"}}}\n\n")
			return
		}
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\"}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 1)
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/responses", "text/event-stream", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if attempts.Load() != 2 || resp.StatusCode != http.StatusOK || strings.Contains(string(body), "response.failed") || !strings.Contains(string(body), "response.output_text.delta") {
		t.Fatalf("attempts=%d status=%d body=%q", attempts.Load(), resp.StatusCode, body)
	}
}

func TestSSEDiagnosticsReportErrorEvent(t *testing.T) {
	logs := captureProxyLogs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"code\":\"model_overloaded\",\"message\":\"try again later\"}\n\n")
	}))
	defer upstream.Close()
	server := testProxy(t, upstream, 0)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	got := logs.String()
	for _, want := range []string{
		"terminal=\"error\"",
		"upstream_error_code=\"model_overloaded\"",
		"upstream_error_message=\"try again later\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostic log missing %q in %q", want, got)
		}
	}
}

type diagnosticErrorReader struct {
	data []byte
	done bool
}

func (r *diagnosticErrorReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		n := copy(p, r.data)
		return n, nil
	}
	return 0, errors.New("simulated upstream connection reset")
}

func (r *diagnosticErrorReader) Close() error { return nil }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSSEDiagnosticsReportMidstreamDisconnect(t *testing.T) {
	logs := captureProxyLogs(t)
	u, _ := url.Parse("http://upstream.invalid/v1")
	p := &proxy{
		cfg: config{upstream: u, maxRetries: 0, backoff: 0, maxRetryWait: time.Second},
		client: &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       &diagnosticErrorReader{data: []byte("data: first\n\n")},
			}, nil
		})},
	}
	server := httptest.NewServer(p)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "data: first") {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	got := logs.String()
	for _, want := range []string{
		"stream=incomplete",
		"transport_error=\"simulated upstream connection reset\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostic log missing %q in %q", want, got)
		}
	}
}
