package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// A pull request review is the approval GitHub already has a UI, an audit
// trail, and branch protection for. Reading it means the gate can fail closed
// on a permission expansion without anyone committing a ledger file, and the
// pull request that widens a tool cannot also carry the file that unblocks it.

// GitHubOptions says which pull request to read and how to reach the API.
type GitHubOptions struct {
	// APIURL is the REST root, https://api.github.com unless GitHub Enterprise.
	APIURL string
	Token  string
	// Repo is owner/name.
	Repo string
	PR   int
	// Client is optional; a 20s-timeout client is used when nil.
	Client *http.Client
}

// Approver is a reviewer whose review stands as the human sign-off.
type Approver struct {
	Login string `json:"login"`
	// Role is the reviewer's repository role: admin, maintain, or write.
	Role string `json:"role"`
	// CommitID is the commit the review was left on, always the head commit.
	CommitID string `json:"commit_id"`
}

// GitHubEnv reads the options GitHub Actions provides: GITHUB_TOKEN,
// GITHUB_REPOSITORY, GITHUB_API_URL, and the pull request number from the event
// payload at GITHUB_EVENT_PATH. pr overrides the payload when non-zero, for
// workflows whose event carries no pull request.
func GitHubEnv(getenv func(string) string, pr int) (GitHubOptions, error) {
	opt := GitHubOptions{
		APIURL: getenv("GITHUB_API_URL"),
		Token:  getenv("GITHUB_TOKEN"),
		Repo:   getenv("GITHUB_REPOSITORY"),
		PR:     pr,
	}
	var missing []string
	if opt.Token == "" {
		missing = append(missing, "GITHUB_TOKEN")
	}
	if opt.Repo == "" {
		missing = append(missing, "GITHUB_REPOSITORY")
	}
	if opt.PR == 0 {
		if path := getenv("GITHUB_EVENT_PATH"); path != "" {
			n, err := prNumberFromEvent(path)
			if err != nil {
				return opt, err
			}
			opt.PR = n
		}
	}
	if opt.PR == 0 {
		missing = append(missing, "a pull request (--pr N, or GITHUB_EVENT_PATH with a pull_request payload)")
	}
	if len(missing) > 0 {
		return opt, fmt.Errorf("github approvals need %s", strings.Join(missing, ", "))
	}
	return opt, nil
}

func prNumberFromEvent(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read event payload: %w", err)
	}
	var ev struct {
		PullRequest *struct {
			Number int `json:"number"`
		} `json:"pull_request"`
		Issue *struct {
			Number      int             `json:"number"`
			PullRequest json.RawMessage `json:"pull_request"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return 0, fmt.Errorf("event payload: %w", err)
	}
	switch {
	case ev.PullRequest != nil && ev.PullRequest.Number > 0:
		return ev.PullRequest.Number, nil
	case ev.Issue != nil && ev.Issue.Number > 0 && len(ev.Issue.PullRequest) > 0:
		return ev.Issue.Number, nil
	}
	return 0, nil
}

// GitHubReviewApprovals returns the reviewers whose current review approves the
// pull request's head commit and who can push to the repository.
//
// Only a reviewer's latest APPROVED or CHANGES_REQUESTED review counts, the way
// GitHub itself reads them; a COMMENTED review changes nothing, a DISMISSED one
// withdraws. An approval left on an earlier commit does not carry to the
// current head: the change under review might not be the one that was
// approved. The pull request author never approves their own change. A
// reviewer's access is checked through the collaborator permission endpoint,
// so an organisation member who can only read does not unblock a merge.
//
// An empty result with a nil error means no approval stands. Any error means
// the approval could not be established, and callers should fail closed.
func GitHubReviewApprovals(ctx context.Context, opt GitHubOptions) ([]Approver, error) {
	if opt.Repo == "" || opt.PR <= 0 {
		return nil, errors.New("github approvals: repository and pull request number are required")
	}
	c := &ghClient{opt: opt}

	var pr struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/pulls/%d", opt.Repo, opt.PR), &pr); err != nil {
		return nil, err
	}
	if pr.Head.SHA == "" {
		return nil, fmt.Errorf("github approvals: pull request %d has no head commit", opt.PR)
	}

	type review struct {
		State    string `json:"state"`
		CommitID string `json:"commit_id"`
		User     struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	// Latest state-bearing review per reviewer. Reviews come back oldest
	// first, so the last one seen wins.
	latest := map[string]review{}
	var order []string
	for page := 1; ; page++ {
		var batch []review
		path := fmt.Sprintf("/repos/%s/pulls/%d/reviews?per_page=100&page=%d", opt.Repo, opt.PR, page)
		if err := c.get(ctx, path, &batch); err != nil {
			return nil, err
		}
		for _, r := range batch {
			switch strings.ToUpper(r.State) {
			case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
			default:
				continue
			}
			if _, seen := latest[r.User.Login]; !seen {
				order = append(order, r.User.Login)
			}
			latest[r.User.Login] = r
		}
		if len(batch) < 100 {
			break
		}
	}

	var approvers []Approver
	for _, login := range order {
		r := latest[login]
		if !strings.EqualFold(r.State, "APPROVED") || r.CommitID != pr.Head.SHA {
			continue
		}
		if login == "" || strings.EqualFold(login, pr.User.Login) {
			continue
		}
		role, err := c.role(ctx, login)
		if err != nil {
			return nil, err
		}
		switch role {
		case "admin", "maintain", "write":
		default:
			continue
		}
		approvers = append(approvers, Approver{Login: login, Role: role, CommitID: r.CommitID})
	}
	return approvers, nil
}

// FormatApprovers is the one-line note for a terminal or a comment.
func FormatApprovers(a []Approver) string {
	if len(a) == 0 {
		return ""
	}
	names := make([]string, 0, len(a))
	for _, x := range a {
		names = append(names, "@"+x.Login)
	}
	return strings.Join(names, ", ") + " (pull request review on " + short(a[0].CommitID) + ")"
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

type ghClient struct {
	opt GitHubOptions
}

func (c *ghClient) role(ctx context.Context, login string) (string, error) {
	var perm struct {
		Permission string `json:"permission"`
		RoleName   string `json:"role_name"`
	}
	path := fmt.Sprintf("/repos/%s/collaborators/%s/permission", c.opt.Repo, login)
	if err := c.get(ctx, path, &perm); err != nil {
		return "", err
	}
	// role_name knows about maintain and triage; permission collapses them to
	// write and read. Prefer the finer one when the server sends it.
	if perm.RoleName != "" {
		return strings.ToLower(perm.RoleName), nil
	}
	return strings.ToLower(perm.Permission), nil
}

func (c *ghClient) get(ctx context.Context, path string, into any) error {
	base := strings.TrimRight(c.opt.APIURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "airlock")
	if c.opt.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.opt.Token)
	}
	client := c.opt.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("github approvals: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("github approvals: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(body))
		var ge struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &ge) == nil && ge.Message != "" {
			msg = ge.Message
		}
		return fmt.Errorf("github approvals: GET %s: %s: %s", path, strconv.Itoa(resp.StatusCode), msg)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("github approvals: GET %s: %w", path, err)
	}
	return nil
}
