package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	ghclient "github.com/hpdkhoa/gen-system/generator/internal/github"
)

// GetGitHubConfig returns the current GitHub integration config (token redacted).
func (h *Handlers) GetGitHubConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := ghclient.LoadConfig()
	if err != nil {
		writeJSON(w, map[string]interface{}{
			"configured": false,
			"error":      err.Error(),
		})
		return
	}
	client := ghclient.NewClient(cfg)
	writeJSON(w, map[string]interface{}{
		"configured": true,
		"config":     client.SafeConfig(),
	})
}

// SetGitHubConfig accepts a JSON body to update runtime GitHub config.
// In practice, the env vars (GH_TOKEN, GH_OWNER, GH_REPO) are the
// primary config mechanism; this endpoint is for Studio UI convenience.
func (h *Handlers) SetGitHubConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Base  string `json:"base"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	// Env vars are the source of truth; log the update suggestion.
	fmt.Printf("  [github] Config update requested: owner=%s repo=%s base=%s\n",
		req.Owner, req.Repo, req.Base)
	writeJSON(w, map[string]string{
		"status": "set GH_OWNER, GH_REPO, GH_BASE env vars and restart server",
	})
}

// TriggerPR creates a GitHub PR for the last successful build.
// Called from the Studio UI "Create PR" button after a completed build.
func (h *Handlers) TriggerPR(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Goal     string `json:"goal"`
		RootPath string `json:"root_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if req.Goal == "" {
		http.Error(w, "goal required", 400)
		return
	}
	if req.RootPath == "" {
		req.RootPath = "."
	}

	result, err := ghclient.CreateBuildPR(req.RootPath, req.Goal)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}

	h.broker.Publish("pr_created", map[string]interface{}{
		"number": result.Number,
		"url":    result.URL,
		"branch": result.Branch,
		"files":  result.Files,
	})

	writeJSON(w, result)
}

// GitHubWebhook handles incoming GitHub webhook events.
// Publishes SSE events to the Studio UI when a gen-system PR is merged.
func (h *Handlers) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	ghclient.WebhookHandler(func(event ghclient.WebhookEvent) {
		// When a gen-system PR is merged, notify the Studio UI
		if event.Event == "pull_request" &&
			event.Action == "closed" &&
			event.PullRequest.Merged {

			h.broker.Publish("pr_merged", map[string]interface{}{
				"number": event.PullRequest.Number,
				"title":  event.PullRequest.Title,
				"repo":   event.Repository.FullName,
			})
			fmt.Printf("  [github/webhook] ✅ PR #%d merged: %s\n",
				event.PullRequest.Number, event.PullRequest.Title)
		}
	})(w, r)
}
