// Package github provides a minimal GitHub REST API client for gen-system.
// Intentionally avoids external dependencies — uses only net/http + encoding/json.
//
// Phase 3.1 — HITL CI/CD:
//
//	After a successful build, gen-system can open a GitHub PR so a human
//	can review and approve the generated code before it lands on main.
//
// Configuration (via environment variables or /api/github/config):
//
//	GH_TOKEN    GitHub personal access token (repo scope)
//	GH_OWNER    Repository owner (user or org)
//	GH_REPO     Repository name
//	GH_BASE     Base branch for PRs (default: main)
package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const apiBase = "https://api.github.com"

// Config holds GitHub integration settings.
// Loaded from env vars; overrideable at runtime via /api/github/config.
type Config struct {
	Token string `json:"token"` // GH_TOKEN — never logged
	Owner string `json:"owner"` // GH_OWNER
	Repo  string `json:"repo"`  // GH_REPO
	Base  string `json:"base"`  // GH_BASE, default "main"
}

// LoadConfig reads GitHub config from environment variables.
// Returns an error if required fields (token, owner, repo) are missing.
func LoadConfig() (Config, error) {
	cfg := Config{
		Token: os.Getenv("GH_TOKEN"),
		Owner: os.Getenv("GH_OWNER"),
		Repo:  os.Getenv("GH_REPO"),
		Base:  os.Getenv("GH_BASE"),
	}
	if cfg.Base == "" {
		cfg.Base = "main"
	}
	if cfg.Token == "" || cfg.Owner == "" || cfg.Repo == "" {
		return cfg, fmt.Errorf("github: GH_TOKEN, GH_OWNER, and GH_REPO must be set")
	}
	return cfg, nil
}

// Client is a thin GitHub REST API wrapper.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient returns a Client ready to make authenticated GitHub API requests.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// do executes a GitHub API request and decodes the JSON response into dst.
// dst may be nil if the caller doesn't need the response body.
func (c *Client) do(method, path string, body interface{}, dst interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("github: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, apiBase+path, bodyReader)
	if err != nil {
		return fmt.Errorf("github: build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github: http: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("github API %s %s → %d: %s",
			method, path, resp.StatusCode, string(respBytes))
	}

	if dst != nil {
		if err := json.Unmarshal(respBytes, dst); err != nil {
			return fmt.Errorf("github: decode response: %w", err)
		}
	}
	return nil
}

// GetRef returns the current SHA of a git ref (e.g. "refs/heads/main").
func (c *Client) GetRef(ref string) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/git/ref/%s", c.cfg.Owner, c.cfg.Repo, ref)
	var resp struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.do("GET", path, nil, &resp); err != nil {
		return "", err
	}
	return resp.Object.SHA, nil
}

// CreateRef creates a new git branch at the given SHA.
func (c *Client) CreateRef(branchName, sha string) error {
	path := fmt.Sprintf("/repos/%s/%s/git/refs", c.cfg.Owner, c.cfg.Repo)
	body := map[string]string{
		"ref": "refs/heads/" + branchName,
		"sha": sha,
	}
	return c.do("POST", path, body, nil)
}

// UpsertFile creates or updates a single file in the repository.
// message is the commit message. If sha is non-empty, the file is updated;
// if empty, it's created.
func (c *Client) UpsertFile(filePath, content, message, branch, sha string) error {
	path := fmt.Sprintf("/repos/%s/%s/contents/%s", c.cfg.Owner, c.cfg.Repo, filePath)
	body := map[string]string{
		"message": message,
		"content": content, // base64-encoded by caller
		"branch":  branch,
	}
	if sha != "" {
		body["sha"] = sha
	}
	return c.do("PUT", path, body, nil)
}

// PRRequest is the payload for creating a pull request.
type PRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"` // branch to merge from
	Base  string `json:"base"` // branch to merge into
	Draft bool   `json:"draft"`
}

// PRResponse is the GitHub API response for a created pull request.
type PRResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

// CreatePR opens a pull request and returns the PR number and URL.
func (c *Client) CreatePR(req PRRequest) (PRResponse, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls", c.cfg.Owner, c.cfg.Repo)
	var resp PRResponse
	if err := c.do("POST", path, req, &resp); err != nil {
		return PRResponse{}, err
	}
	return resp, nil
}

// Config returns a copy of the client's current configuration
// with the token redacted for safe logging.
func (c *Client) SafeConfig() map[string]string {
	return map[string]string{
		"owner": c.cfg.Owner,
		"repo":  c.cfg.Repo,
		"base":  c.cfg.Base,
		"token": "***",
	}
}
