package depllo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Event types Depllo delivers to webhook endpoints (GET /webhook-endpoints/event-types).
const (
	EventPipelineFinished        = "depllo.pipeline.finished.v1"
	EventJobFinished             = "depllo.job.finished.v1"
	EventWebhookEndpointDisabled = "depllo.webhook_endpoint.disabled.v1"
)

// WebhookEvent is the body of every delivery. Data is the event's payload; decode it
// with json.Unmarshal into the shape its Type documents. ID (evt_…) is the same on every
// attempt — use it to drop duplicates.
type WebhookEvent struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurredAt"`
	AccountID  string          `json:"accountId"`
	Data       json.RawMessage `json:"data"`
}

// VerifyWebhook checks a delivery's Depllo-Signature header
// (t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>) against the RAW request body
// and returns the event. tolerance is how far the timestamp may be from now (0 means
// 5 minutes). It returns an *Error with Code "invalid_signature" on a missing or
// malformed header, a stale timestamp, a signature that does not match, or a body that
// is not JSON.
func VerifyWebhook(rawBody []byte, signature, secret string, tolerance time.Duration) (*WebhookEvent, error) {
	return verifyWebhookAt(rawBody, signature, secret, tolerance, time.Now())
}

func verifyWebhookAt(rawBody []byte, signature, secret string, tolerance time.Duration, now time.Time) (*WebhookEvent, error) {
	fail := func(format string, args ...any) error {
		return &Error{Status: 400, Code: "invalid_signature", Message: fmt.Sprintf(format, args...)}
	}
	if tolerance <= 0 {
		tolerance = 5 * time.Minute
	}
	if signature == "" {
		return nil, fail("missing Depllo-Signature header")
	}
	parts := map[string]string{}
	for _, seg := range strings.Split(signature, ",") {
		if k, v, ok := strings.Cut(seg, "="); ok {
			parts[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	ts, err := strconv.ParseInt(parts["t"], 10, 64)
	if err != nil || parts["v1"] == "" {
		return nil, fail("malformed Depllo-Signature header")
	}
	drift := now.Sub(time.Unix(ts, 0))
	if drift < 0 {
		drift = -drift
	}
	if drift > tolerance {
		return nil, fail("signature timestamp is %ds from now", int(drift.Seconds()))
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts["t"] + "."))
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts["v1"])) {
		return nil, fail("signature does not match")
	}
	var ev WebhookEvent
	if err := json.Unmarshal(rawBody, &ev); err != nil {
		return nil, fail("webhook body is not valid JSON")
	}
	return &ev, nil
}
