package servicecheck

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gabrielagostinho/pulse/internal/domain/service"
)

const maxBody = 64 << 10

type Checker struct {
	client   *http.Client
	insecure *http.Client
	dialer   *net.Dialer
	now      func() time.Time
}

func New() *Checker {
	mk := func(skipVerify bool) *http.Client {
		tr := &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   2,
			MaxConnsPerHost:       4,
			ForceAttemptHTTP2:     true,
		}
		if skipVerify {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
		}
		return &http.Client{
			Transport: tr,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				return nil
			},
		}
	}
	return &Checker{client: mk(false), insecure: mk(true), dialer: &net.Dialer{}, now: time.Now}
}

func (c *Checker) Check(ctx context.Context, def service.Definition) service.Probe {
	timeout := def.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch def.Kind {
	case service.KindTCP:
		return c.tcp(ctx, def)
	default:
		return c.http(ctx, def)
	}
}

func (c *Checker) http(ctx context.Context, def service.Definition) service.Probe {
	start := c.now()
	p := service.Probe{At: start}
	method := def.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, def.Target, nil)
	if err != nil {
		p.Err = "invalid request: " + err.Error()
		return p
	}
	req.Header.Set("User-Agent", "pulse-service-watch/1")
	for k, v := range def.Headers {
		req.Header.Set(k, v)
	}
	client := c.client
	if def.TLSSkipVerify {
		client = c.insecure
	}
	resp, err := client.Do(req)
	if err != nil {
		p.Latency = time.Since(start)
		p.Err = describe(err, def.Timeout)
		return p
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	_ = resp.Body.Close()
	p.Latency = time.Since(start)
	p.StatusCode = resp.StatusCode
	if def.AcceptsStatus(resp.StatusCode) {
		p.OK = true
	} else {
		p.Err = fmt.Sprintf("unexpected status %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return p
}

func (c *Checker) tcp(ctx context.Context, def service.Definition) service.Probe {
	start := c.now()
	p := service.Probe{At: start}
	conn, err := c.dialer.DialContext(ctx, "tcp", def.Target)
	p.Latency = time.Since(start)
	if err != nil {
		p.Err = describe(err, def.Timeout)
		return p
	}
	_ = conn.Close()
	p.OK = true
	return p
}

func describe(err error, timeout time.Duration) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		err = uerr.Err
	}
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return fmt.Sprintf("timed out after %s", timeout)
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return "DNS: host not found (" + dnsErr.Name + ")"
		}
		return "DNS: " + dnsErr.Err
	case isRefused(err) || strings.Contains(err.Error(), "refused"):
		return "connection refused"
	case isReset(err) || strings.Contains(err.Error(), "reset by peer"):
		return "connection reset by peer"
	case isUnreachable(err):
		return "network unreachable"
	case strings.Contains(err.Error(), "certificate"):
		return "TLS: " + trimPrefix(err.Error())
	}
	return trimPrefix(err.Error())
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func trimPrefix(s string) string {
	if i := strings.LastIndex(s, ": "); i > 0 && strings.HasPrefix(s, "dial ") {
		return s[i+2:]
	}
	return s
}
