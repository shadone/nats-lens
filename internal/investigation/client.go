package investigation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

const MaxResponseBytes = 1 << 20

// Profiles are operator-controlled configuration, never copied browser data.
// ASVS 13.3.1/13.3.2, 14.2.1: store only a protected environment reference;
// credentials go in Authorization, never URLs, context or command arguments.
type Profile struct {
	Deployment string `json:"deployment"`
	Endpoint   string `json:"endpoint"`
	TokenEnv   string `json:"tokenEnv,omitempty"`
}

// Source is the single command-facing interface. A future offline source must
// invoke the authoritative API validation/finishing, not parallel SQL logic.
type Source interface {
	Query(context.Context, string, []byte) (json.RawMessage, error)
}
type Remote struct {
	endpoint string
	token    string
	http     *http.Client
}

var envName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)

func NewRemote(p Profile) (*Remote, error) {
	if _, err := origin(p.Deployment); err != nil {
		return nil, err
	}
	u, err := origin(p.Endpoint)
	if err != nil {
		return nil, errors.New("profile endpoint must be an HTTP(S) origin without path, credentials or query")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, errors.New("HTTP is allowed only for an explicitly configured loopback endpoint; use HTTPS for remote access")
		}
	}
	token := ""
	if p.TokenEnv != "" {
		if !envName.MatchString(p.TokenEnv) {
			return nil, errors.New("invalid tokenEnv reference")
		}
		token = os.Getenv(p.TokenEnv)
		if token == "" || !validText(token, 16384) {
			return nil, errors.New("profile credential is unavailable; configure authorized proxy access independently of browser login")
		}
	}
	// No redirects, cookies, ambient proxy or .netrc credentials. The trusted
	// endpoint can differ from the browser origin only in operator configuration.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Remote{endpoint: u.String(), token: token, http: &http.Client{Transport: transport, Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}}, nil
}

type safeHTTP struct{ r *Remote }

func (s safeHTTP) Do(req *http.Request) (*http.Response, error) {
	if s.r.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.r.token)
	}
	res, err := s.r.http.Do(req)
	if err != nil {
		return nil, errors.New("endpoint unavailable, redirect refused, or request cancelled; verify profile/network access")
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		code := connect.CodeUnavailable
		switch res.StatusCode {
		case 401:
			code = connect.CodeUnauthenticated
		case 403:
			code = connect.CodePermissionDenied
		case 404:
			code = connect.CodeUnimplemented
		case 301, 302, 303, 307, 308:
			code = connect.CodeFailedPrecondition
		}
		return nil, connect.NewError(code, errors.New("endpoint refused request; verify authorized profile and server version"))
	}
	// Connect also enforces a decompressed protobuf-message limit below. Bound
	// non-Connect HTTP bodies before decoding and never expose proxy error text.
	res.Body = &boundedBody{ReadCloser: res.Body, left: MaxResponseBytes + 1}
	return res, nil
}

type boundedBody struct {
	io.ReadCloser
	left int
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, errors.New("response exceeds 1 MiB; narrow the query")
	}
	if len(p) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= n
	return n, err
}
func (r *Remote) Query(ctx context.Context, method string, request []byte) (json.RawMessage, error) {
	md := Method(method)
	if md == nil {
		return nil, errors.New("method is unavailable on the read-only investigation surface")
	}
	if len(request) > MaxInputBytes {
		return nil, errors.New("request exceeds 64 KiB")
	}
	req := dynamicpb.NewMessage(md.Input())
	if err := protojson.Unmarshal(request, req); err != nil {
		return nil, errors.New("request does not match the method schema; run investigate describe METHOD")
	}
	c := connect.NewClient[dynamicpb.Message, dynamicpb.Message](safeHTTP{r}, r.endpoint+Path+"/"+Service+"/"+method,
		connect.WithProtoJSON(), connect.WithReadMaxBytes(MaxResponseBytes), connect.WithSendMaxBytes(MaxInputBytes),
		connect.WithResponseInitializer(func(_ connect.Spec, message any) error {
			*message.(*dynamicpb.Message) = *dynamicpb.NewMessage(md.Output())
			return nil
		}))
	res, err := c.CallUnary(ctx, connect.NewRequest(req))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Server/proxy error messages can contain untrusted evidence or secrets.
		code := connect.CodeOf(err)
		return nil, errors.New("query failed (" + code.String() + "); check access/server compatibility, narrow scope, or inspect Data history for retention loss")
	}
	if err := checkEventSelectionReply(method, req.ProtoReflect(), res.Msg.ProtoReflect()); err != nil {
		return nil, err
	}
	// Standard protobuf JSON preserves int64/uint64 as decimal strings, optional
	// presence. Fields newer than this client schema are unavailable.
	return protojson.MarshalOptions{EmitDefaultValues: true}.Marshal(res.Msg)
}
func LoadProfile(r io.Reader, name string) (Profile, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxInputBytes+1))
	if err != nil || len(data) > MaxInputBytes {
		return Profile{}, errors.New("invalid or oversized profiles file")
	}
	var profiles map[string]Profile
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err = d.Decode(&profiles); err != nil || d.Decode(new(any)) != io.EOF {
		return Profile{}, errors.New("invalid profiles JSON")
	}
	p, ok := profiles[name]
	if !ok {
		return p, errors.New("profile is not configured")
	}
	return p, nil
}

// ValidateURL never uses a page URL as a network destination.
func (p Profile) ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return errors.New("invalid page URL")
	}
	t, err := origin(p.Deployment)
	if err != nil || u.Scheme != t.Scheme || u.Host != t.Host {
		return errors.New("page URL does not match trusted deployment")
	}
	return validateSource(u.RequestURI())
}
