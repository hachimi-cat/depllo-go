package depllo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testSecret = "whsec_test_secret"

var testBody = []byte(`{"id":"evt_1","type":"depllo.pipeline.finished.v1","occurredAt":"2026-10-01T12:00:00.000Z","accountId":"acc_1","data":{"status":"success"}}`)

func sign(t int64, body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", t)))
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(mac.Sum(nil)))
}

func TestVerifyWebhook(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	ev, err := verifyWebhookAt(testBody, sign(now.Unix(), testBody, testSecret), testSecret, 0, now)
	if err != nil {
		t.Fatalf("good signature refused: %v", err)
	}
	if ev.ID != "evt_1" || ev.Type != EventPipelineFinished || ev.AccountID != "acc_1" || !strings.Contains(string(ev.Data), "success") {
		t.Fatalf("unexpected event %+v", ev)
	}

	cases := []struct {
		name, body, sig, want string
	}{
		{"wrong secret", string(testBody), sign(now.Unix(), testBody, "whsec_other"), "does not match"},
		{"changed body", strings.Replace(string(testBody), "success", "failed", 1), sign(now.Unix(), testBody, testSecret), "does not match"},
		{"stale", string(testBody), sign(now.Unix()-301, testBody, testSecret), "from now"},
		{"missing", string(testBody), "", "missing"},
		{"malformed", string(testBody), "v1=abc", "malformed"},
	}
	for _, c := range cases {
		_, err := verifyWebhookAt([]byte(c.body), c.sig, testSecret, 0, now)
		var e *Error
		if !errors.As(err, &e) || e.Code != "invalid_signature" || !strings.Contains(e.Message, c.want) {
			t.Errorf("%s: got %v", c.name, err)
		}
	}
	if _, err := verifyWebhookAt(testBody, sign(now.Unix()-301, testBody, testSecret), testSecret, 10*time.Minute, now); err != nil {
		t.Errorf("tolerance not honoured: %v", err)
	}
}
