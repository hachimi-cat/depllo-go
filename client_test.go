package depllo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type seenRequest struct {
	method, path, rawPath, query, auth, idem, contentType string
	body                                                  []byte
}

// server records every request and answers with reply; the client's base URL ends in
// /api/v1 like the default.
func server(t *testing.T, reply func(w http.ResponseWriter, r *http.Request)) (*Client, *[]seenRequest, func()) {
	t.Helper()
	var seen []seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, seenRequest{
			method: r.Method, path: r.URL.Path, rawPath: r.URL.EscapedPath(), query: r.URL.RawQuery,
			auth: r.Header.Get("Authorization"), idem: r.Header.Get("Idempotency-Key"),
			contentType: r.Header.Get("Content-Type"), body: body,
		})
		reply(w, r)
	}))
	return New(Config{Token: "sk_live_test", BaseURL: srv.URL + "/api/v1"}), &seen, srv.Close
}

func envelope(w http.ResponseWriter, status int, data any, apiErr any, meta map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "error": apiErr, "meta": meta})
}

func TestBodyBearerAndEnvelope(t *testing.T) {
	c, seen, done := server(t, func(w http.ResponseWriter, r *http.Request) {
		envelope(w, 201, map[string]any{"id": "cvar_1"}, nil, map[string]any{"requestId": "req_1"})
	})
	defer done()
	raw, err := c.API.ProjectsCreateVariables(context.Background(), "proj_1", &ProjectsCreateVariablesArgs{
		Key: "API_URL", Value: "https://x", Masked: Ptr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := (*seen)[0]
	if s.method != "POST" || s.path != "/api/v1/projects/proj_1/variables" {
		t.Errorf("request = %s %s", s.method, s.path)
	}
	if s.auth != "Bearer sk_live_test" || !strings.HasPrefix(s.idem, "sdk_") || s.contentType != "application/json" {
		t.Errorf("headers = %+v", s)
	}
	var body map[string]any
	_ = json.Unmarshal(s.body, &body)
	if body["key"] != "API_URL" || body["value"] != "https://x" || body["masked"] != true || len(body) != 3 {
		t.Errorf("body = %v", body)
	}
	// the whole envelope, like the JS and Python SDKs
	env, err := DecodeEnvelope(raw)
	if err != nil || env.Meta == nil || env.Meta.RequestID != "req_1" {
		t.Fatalf("envelope = %s (%v)", raw, err)
	}
	var data struct{ ID string }
	if err := Decode(raw, &data); err != nil || data.ID != "cvar_1" {
		t.Errorf("data = %+v (%v)", data, err)
	}
}

func TestPathEscapingAndQuery(t *testing.T) {
	c, seen, done := server(t, func(w http.ResponseWriter, r *http.Request) {
		envelope(w, 200, []any{}, nil, map[string]any{"cursor": "c2", "hasMore": true})
	})
	defer done()
	ctx := context.Background()
	if _, err := c.API.ProjectsDeleteSchedules(ctx, "proj 1", "sch/2"); err != nil {
		t.Fatal(err)
	}
	raw, err := c.API.JobsLog(ctx, "job_1", &JobsLogArgs{From: Ptr(5)})
	if err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0].rawPath; got != "/api/v1/projects/proj%201/schedules/sch%2F2" {
		t.Errorf("escaped path = %s", got)
	}
	if s := (*seen)[1]; s.path != "/api/v1/jobs/job_1/log" || s.query != "from=5" || s.idem != "" || len(s.body) != 0 {
		t.Errorf("GET = %+v", s)
	}
	env, _ := DecodeEnvelope(raw)
	if env.Meta == nil || env.Meta.Cursor == nil || *env.Meta.Cursor != "c2" || !env.Meta.HasMore {
		t.Errorf("list meta lost: %s", raw)
	}
}

func TestFileRoute(t *testing.T) {
	c, _, done := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="dist.zip"`)
		_, _ = w.Write([]byte("PK\x03\x04"))
	})
	defer done()
	raw, err := c.API.JobsArtifactsDownload(context.Background(), "job_1", "art_1")
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := Decode(raw, &f); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.Data, []byte("PK\x03\x04")) || f.ContentType != "application/zip" || f.Filename != "dist.zip" {
		t.Errorf("file = %+v", f)
	}
}

func TestErrors(t *testing.T) {
	c, _, done := server(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/api-keys") {
			envelope(w, 403, nil, map[string]any{"code": "FORBIDDEN", "message": "API keys cannot manage API keys"}, map[string]any{"requestId": "req_x"})
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	})
	defer done()
	ctx := context.Background()
	var apiErr *Error
	_, err := c.API.APIKeysCreate(ctx, &APIKeysCreateArgs{Name: "catent"})
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || apiErr.Code != "FORBIDDEN" || apiErr.RequestID != "req_x" {
		t.Errorf("envelope error = %#v", err)
	}
	// an HTML page at an API path is a wrong base URL, not a file
	_, err = c.API.ProjectsList(ctx)
	if !errors.As(err, &apiErr) || apiErr.Code != "INVALID_RESPONSE" {
		t.Errorf("html page = %#v", err)
	}
}

func TestRequiredFieldsAndToken(t *testing.T) {
	ctx := context.Background()
	c := New(Config{Token: "t", BaseURL: "http://127.0.0.1:1/api/v1"})
	if _, err := c.API.ProjectsCreatePipelines(ctx, "proj_1", nil); err == nil || !strings.Contains(err.Error(), "Ref") {
		t.Errorf("missing Ref = %v", err)
	}
	t.Setenv("DEPLLO_TOKEN", "")
	_, err := New(Config{BaseURL: "http://127.0.0.1:1/api/v1"}).API.RunnersList(ctx)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "AUTH_REQUIRED" {
		t.Errorf("no token = %v", err)
	}
	t.Setenv("DEPLLO_TOKEN", "sk_live_env")
	if New(Config{}).token != "sk_live_env" || New(Config{}).baseURL != DefaultBaseURL {
		t.Error("defaults not read")
	}
}
