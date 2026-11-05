package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/shadone/nats-lens/internal/config"
)

// Outcome is what a delivery attempt came to.
type Outcome int

const (
	// Delivered: the receiver answered 2xx.
	Delivered Outcome = iota
	// Retry: a 5xx, 408 or 429, a network error, a timeout, or a URL or
	// token that cannot be read now; the same batch may be sent again.
	Retry
	// Refused: any other answer, a redirect included; sending the same
	// batch again would be refused again.
	Refused
)

func (o Outcome) String() string {
	switch o {
	case Delivered:
		return "delivered"
	case Retry:
		return "failed"
	case Refused:
		return "refused"
	}
	return "unknown"
}

// Result is one delivery attempt. Its texts never hold the URL's path or
// query or the token.
type Result struct {
	Outcome Outcome
	// Status is the HTTP status; 0 when no answer came.
	Status int
	// RetryAfter is the receiver's Retry-After; 0 when it sent none.
	RetryAfter time.Duration
	// Host is the receiver's scheme and host; empty when the URL could not
	// be read.
	Host string
	// Err says what went wrong; nil when delivered.
	Err error
}

// String is the result in words: "delivered, HTTP 204", "refused, HTTP
// 404", "failed: no answer within 10s".
func (r Result) String() string {
	s := r.Outcome.String()
	if r.Status != 0 {
		s += ", HTTP " + strconv.Itoa(r.Status)
	}
	if r.Err != nil {
		s += ": " + r.Err.Error()
	}
	return s
}

// maxResponseRead is how much of an answer's body is read before the
// connection is reused.
const maxResponseRead = 64 << 10

// Sender sends batches to one channel.
type Sender struct {
	ch      config.Channel
	version string
	client  *http.Client
	now     func() time.Time
}

// NewSender returns the channel's sender. version is nats-lens's, for the
// User-Agent.
func NewSender(ch config.Channel, version string) *Sender {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	return &Sender{
		ch: ch, version: version, now: time.Now,
		client: &http.Client{
			Transport: tr,
			Timeout:   ch.Timeout.D(),
			// A redirect could carry the token to another host.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Channel is the sender's channel.
func (s *Sender) Channel() config.Channel { return s.ch }

// Send sends the batch once. The URL and token are read from their file
// or variable for this attempt.
func (s *Sender) Send(ctx context.Context, b Batch) Result {
	d, err := readDestination(s.ch)
	if err != nil {
		return Result{Outcome: Retry, Err: err}
	}
	res := Result{Host: config.RedactChannelURL(d.url)}
	var body []byte
	if s.ch.Type == config.ChannelSlack {
		body, err = SlackBody(b)
	} else {
		body, err = WebhookBody(b)
	}
	if err != nil {
		res.Outcome, res.Err = Refused, fmt.Errorf("encode body: %w", err)
		return res
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		res.Outcome, res.Err = Refused, errors.New(d.scrub(unwrapURLError(err).Error()))
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "natslens/"+s.version)
	if s.ch.Type == config.ChannelWebhook {
		if d.token != "" {
			req.Header.Set("Authorization", "Bearer "+d.token)
		}
		if b.Key != "" {
			req.Header.Set("Idempotency-Key", b.Key)
		}
	}
	resp, err := s.client.Do(req)
	if err != nil {
		res.Outcome, res.Err = Retry, errors.New(d.scrub(s.requestError(err)))
		return res
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseRead))
	_ = resp.Body.Close()
	res.Status = resp.StatusCode
	switch c := resp.StatusCode; {
	case c >= 200 && c < 300:
		res.Outcome = Delivered
	case c >= 500, c == http.StatusRequestTimeout, c == http.StatusTooManyRequests:
		res.Outcome = Retry
		res.RetryAfter = retryAfter(resp.Header.Get("Retry-After"), s.now())
	case c >= 300 && c < 400:
		res.Outcome, res.Err = Refused, errors.New("the receiver redirected; redirects are not followed")
	default:
		res.Outcome = Refused
	}
	return res
}

// requestError words a failed request without its URL: a *url.Error's
// text holds the whole URL, so only the error it wraps is used.
func (s *Sender) requestError(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return "no answer within " + s.ch.Timeout.String()
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return unwrapURLError(err).Error()
}

func unwrapURLError(err error) error {
	var ue *url.Error
	for errors.As(err, &ue) {
		err = ue.Err
	}
	return err
}

// MaxRetryAfter is the longest Retry-After read; a longer one is held to
// it, so that a huge value cannot overflow the wait.
const MaxRetryAfter = 24 * time.Hour

// retryAfter reads a Retry-After value: seconds or an HTTP date, at most
// MaxRetryAfter. 0 when it is absent, does not parse or is in the past.
func retryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0
		}
		return time.Duration(min(n, int(MaxRetryAfter/time.Second))) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return min(t.Sub(now), MaxRetryAfter)
	}
	return 0
}
