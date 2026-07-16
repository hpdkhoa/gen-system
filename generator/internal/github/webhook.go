package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// WebhookEvent is the minimal subset of a GitHub webhook payload
// that gen-system cares about.
type WebhookEvent struct {
	Action     string `json:"action"`
	Event      string `json:"-"` // filled from X-GitHub-Event header
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		Title  string `json:"title"`
	} `json:"pull_request"`
}

// WebhookHandler returns an http.HandlerFunc that:
//  1. Verifies the HMAC-SHA256 signature (X-Hub-Signature-256 header)
//  2. Decodes the payload
//  3. Calls onEvent(event) for the caller to handle
//
// Set GH_WEBHOOK_SECRET to the secret configured in GitHub's webhook settings.
// If the env var is not set, signature verification is skipped with a warning.
func WebhookHandler(onEvent func(WebhookEvent)) http.HandlerFunc {
	secret := os.Getenv("GH_WEBHOOK_SECRET")
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
		if err != nil {
			http.Error(w, "read body", 400)
			return
		}

		// HMAC verification
		if secret != "" {
			sig := r.Header.Get("X-Hub-Signature-256")
			if !verifySignature(secret, body, sig) {
				http.Error(w, "signature mismatch", 403)
				return
			}
		} else {
			fmt.Println("  [github/webhook] ⚠️  GH_WEBHOOK_SECRET not set — skipping signature check")
		}

		var event WebhookEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "bad JSON", 400)
			return
		}
		event.Event = r.Header.Get("X-GitHub-Event")

		fmt.Printf("  [github/webhook] event=%s action=%s repo=%s\n",
			event.Event, event.Action, event.Repository.FullName)

		go onEvent(event) // non-blocking

		w.WriteHeader(200)
		fmt.Fprint(w, `{"ok":true}`)
	}
}

// verifySignature checks that the payload matches the HMAC-SHA256 signature
// provided by GitHub in the X-Hub-Signature-256 header.
func verifySignature(secret string, payload []byte, sig string) bool {
	if !strings.HasPrefix(sig, "sha256=") {
		return false
	}
	sig = strings.TrimPrefix(sig, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}
