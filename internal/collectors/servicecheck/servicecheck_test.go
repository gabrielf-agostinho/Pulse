package servicecheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gabrielagostinho/pulse/internal/domain/service"
)

func httpDef(url string) service.Definition {
	return service.Definition{ID: "t", Kind: service.KindHTTP, Target: url, Timeout: time.Second, Method: "GET"}
}

func TestHTTPHealthy(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Token")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	def := httpDef(srv.URL)
	def.Headers = map[string]string{"X-Token": "abc"}
	p := New().Check(context.Background(), def)
	if !p.OK || p.StatusCode != 200 || p.Latency <= 0 || p.Err != "" {
		t.Fatalf("probe = %+v", p)
	}
	if gotHeader != "abc" {
		t.Fatal("custom headers must be sent")
	}
}

func TestHTTPUnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	p := New().Check(context.Background(), httpDef(srv.URL))
	if p.OK || p.StatusCode != 503 || !strings.Contains(p.Err, "503") {
		t.Fatalf("probe = %+v", p)
	}
	def := httpDef(srv.URL)
	def.ExpectStatus = []int{503}
	if p := New().Check(context.Background(), def); !p.OK {
		t.Fatal("explicitly expected status must pass")
	}
}

func TestHTTPTimeoutIsEnforced(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	def := httpDef(srv.URL)
	def.Timeout = 100 * time.Millisecond
	start := time.Now()
	p := New().Check(context.Background(), def)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout not enforced: %s", elapsed)
	}
	if p.OK || !strings.Contains(p.Err, "timed out") {
		t.Fatalf("probe = %+v", p)
	}
}

func TestHTTPCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	def := httpDef(srv.URL)
	def.Timeout = 5 * time.Second
	start := time.Now()
	p := New().Check(ctx, def)
	if time.Since(start) > 2*time.Second || p.OK {
		t.Fatalf("cancellation not honoured: %+v", p)
	}
}

func TestHTTPRedirectLimitAndBodyCap(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/loop" {
			http.Redirect(w, r, srv.URL+"/loop", http.StatusFound)
			return
		}
		for i := 0; i < 1<<12; i++ {
			if _, err := w.Write(make([]byte, 1024)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	if p := New().Check(context.Background(), httpDef(srv.URL+"/loop")); p.OK || !strings.Contains(p.Err, "redirects") {
		t.Fatalf("redirect loop: %+v", p)
	}
	if p := New().Check(context.Background(), httpDef(srv.URL+"/big")); !p.OK {
		t.Fatalf("large body: %+v", p)
	}
}

func TestTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	addr := ln.Addr().String()
	def := service.Definition{ID: "db", Kind: service.KindTCP, Target: addr, Timeout: time.Second}
	if p := New().Check(context.Background(), def); !p.OK {
		t.Fatalf("open port: %+v", p)
	}
	_ = ln.Close()
	p := New().Check(context.Background(), def)
	if p.OK || p.Err != "connection refused" {
		t.Fatalf("closed port must be described as refused on every OS: %+v", p)
	}
}

func TestDNSFailureIsDescribed(t *testing.T) {
	p := New().Check(context.Background(), httpDef("http://pulse-nonexistent.invalid/"))
	if p.OK || !strings.Contains(p.Err, "DNS") {
		t.Fatalf("probe = %+v", p)
	}
}

func TestConcurrentChecksDoNotBlockEachOther(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer fast.Close()
	c := New()
	var wg sync.WaitGroup
	var fastDur time.Duration
	wg.Add(2)
	go func() {
		defer wg.Done()
		d := httpDef(slow.URL)
		d.Timeout = 500 * time.Millisecond
		c.Check(context.Background(), d)
	}()
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		start := time.Now()
		c.Check(context.Background(), httpDef(fast.URL))
		fastDur = time.Since(start)
	}()
	wg.Wait()
	if fastDur > 300*time.Millisecond {
		t.Fatalf("fast check was blocked by slow one: %s", fastDur)
	}
}
