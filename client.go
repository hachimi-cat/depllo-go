// Package depllo is the Go SDK for Depllo — GitLab-CI-style CI/CD for GitHub repos.
// Sister to @forjio/depllo (JS) and forjio-depllo (Python).
//
// Auth = Bearer token — a workspace API key (sk_live_…, Dashboard → API Keys) or a
// Huudis access token. Pass Config.Token or set DEPLLO_TOKEN.
//
// Every feature route is on Client.API (generated from the API spec), one method each.
// Like the JS and Python SDKs, a call returns the whole Forjio envelope
// {data, error, meta} as JSON — so a list's meta.cursor / meta.hasMore is kept; read it
// with Decode (the data) or DecodeEnvelope. A route that answers with bytes (a job
// artifact, a badge SVG) has a File as its data. A failed call returns *Error.
package depllo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// DefaultBaseURL is the API base the client talks to unless Config.BaseURL says otherwise.
const DefaultBaseURL = "https://depllo.forjio.com/api/v1"

// Client is the Depllo client.
type Client struct {
	token   string
	baseURL string
	httpc   *http.Client

	// API has every feature route of the Depllo API, one method each (generated from
	// the API spec: api_generated.go).
	API *GeneratedAPI
}

// Config holds the credentials and endpoint overrides.
type Config struct {
	// Token is the Bearer token: an sk_live_… API key (or a Huudis access token).
	// Defaults to the DEPLLO_TOKEN env var.
	Token string
	// BaseURL overrides the API base. Default: https://depllo.forjio.com/api/v1.
	BaseURL string
	// HTTP overrides the http.Client. Default: 30s timeout.
	HTTP *http.Client
}

// Envelope is the Forjio response envelope every call returns.
type Envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Meta *struct {
		RequestID string  `json:"requestId,omitempty"`
		Timestamp string  `json:"timestamp,omitempty"`
		Cursor    *string `json:"cursor,omitempty"`
		HasMore   bool    `json:"hasMore,omitempty"`
	} `json:"meta,omitempty"`
}

// File is the data of a route that answers with bytes (a job artifact, a badge SVG).
type File struct {
	Data        []byte `json:"data"`
	ContentType string `json:"contentType"`
	Filename    string `json:"filename,omitempty"`
}

// DecodeEnvelope reads a call's result as an Envelope.
func DecodeEnvelope(raw json.RawMessage) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// Decode reads the data of a call's result into out.
//
//	raw, err := c.API.ProjectsList(ctx)
//	var projects []map[string]any
//	err = depllo.Decode(raw, &projects)
func Decode(raw json.RawMessage, out any) error {
	env, err := DecodeEnvelope(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(env.Data, out)
}

// New constructs a Depllo client.
func New(cfg Config) *Client {
	token := cfg.Token
	if token == "" {
		token = os.Getenv("DEPLLO_TOKEN")
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	httpc := cfg.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}
	c := &Client{token: token, baseURL: strings.TrimRight(base, "/"), httpc: httpc}
	c.API = &GeneratedAPI{c: c}
	return c
}

// textPage is a response that is a page, not a file: an HTML or plain-text answer at an
// API path is a wrong base URL or a proxy, and stays an error.
var textPage = regexp.MustCompile(`(?i)json|text/(html|plain)`)

func idempotencyKey() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("sdk_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

// apigenRequest is the call behind every GeneratedAPI method: the same bearer token,
// idempotency key and envelope as the JS and Python SDKs. The spec's paths carry the
// /api/v1 prefix the base URL already ends in. It returns the whole envelope.
func (c *Client) apigenRequest(ctx context.Context, method, path string, query url.Values, body map[string]any) (json.RawMessage, error) {
	path = strings.TrimPrefix(path, "/api/v1")
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, &Error{Code: "SERIALIZE_FAILED", Message: err.Error()}
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, &Error{Code: "REQUEST_BUILD_FAILED", Message: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	if c.token == "" {
		return nil, &Error{Code: "AUTH_REQUIRED", Message: "no token configured: set Config.Token or DEPLLO_TOKEN"}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Idempotency-Key", idempotencyKey())
	}

	res, err := c.httpc.Do(req)
	if err != nil {
		return nil, &Error{Code: "NETWORK_ERROR", Message: err.Error()}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, &Error{Status: res.StatusCode, Code: "NETWORK_ERROR", Message: err.Error()}
	}

	contentType := res.Header.Get("Content-Type")
	if res.StatusCode < 400 && contentType != "" && !textPage.MatchString(contentType) {
		f := File{Data: raw, ContentType: contentType}
		if _, params, err := mime.ParseMediaType(res.Header.Get("Content-Disposition")); err == nil {
			f.Filename = params["filename"]
		}
		data, _ := json.Marshal(f)
		out, _ := json.Marshal(map[string]any{"data": json.RawMessage(data), "error": nil})
		return out, nil
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		if res.StatusCode >= 400 {
			return nil, &Error{Status: res.StatusCode, Code: "UNKNOWN", Message: fmt.Sprintf("HTTP %d", res.StatusCode)}
		}
		return nil, &Error{Status: res.StatusCode, Code: "INVALID_RESPONSE", Message: "empty or non-JSON response from server"}
	}
	if res.StatusCode >= 400 || env.Error != nil {
		e := &Error{Status: res.StatusCode, Code: "UNKNOWN", Message: fmt.Sprintf("HTTP %d", res.StatusCode)}
		if env.Error != nil {
			if env.Error.Code != "" {
				e.Code = env.Error.Code
			}
			if env.Error.Message != "" {
				e.Message = env.Error.Message
			}
		}
		if env.Meta != nil {
			e.RequestID = env.Meta.RequestID
		}
		return nil, e
	}
	return raw, nil
}
