package codingworkflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGitHubAPIBase          = "https://api.github.com"
	defaultGitHubMaxResponseBytes = int64(2 << 20)
	defaultGitHubTimeout          = 8 * time.Second
	maxGitHubPRFiles              = 100
	maxGitHubCheckRuns            = 100
)

type GitHubClient struct {
	base             *url.URL
	client           *http.Client
	maxResponseBytes int64
}

type GitHubIssue struct {
	Number      int              `json:"number"`
	State       string           `json:"state"`
	Title       string           `json:"title"`
	Body        string           `json:"body"`
	HTMLURL     string           `json:"html_url"`
	PullRequest *json.RawMessage `json:"pull_request,omitempty"`
}

type GitHubPullRequest struct {
	Number       int    `json:"number"`
	State        string `json:"state"`
	Draft        bool   `json:"draft"`
	HTMLURL      string `json:"html_url"`
	ChangedFiles int    `json:"changed_files"`
	Base         struct {
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
	Head struct {
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

type GitHubPRFile struct {
	SHA       string `json:"sha"`
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
}

type GitHubCheckRun struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	HeadSHA     string `json:"head_sha"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	CompletedAt string `json:"completed_at"`
	HTMLURL     string `json:"html_url"`
}

type githubCheckRunsResponse struct {
	TotalCount int              `json:"total_count"`
	CheckRuns  []GitHubCheckRun `json:"check_runs"`
}

type GitHubHTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (err *GitHubHTTPError) Error() string {
	if err.Body == "" {
		return fmt.Sprintf("GitHub API returned HTTP %d %s", err.StatusCode, err.Status)
	}
	return fmt.Sprintf("GitHub API returned HTTP %d %s: %s", err.StatusCode, err.Status, err.Body)
}

func NewPublicGitHubClient() (*GitHubClient, error) {
	return newGitHubClient(defaultGitHubAPIBase, defaultGitHubTimeout, defaultGitHubMaxResponseBytes, false)
}

func newGitHubClient(base string, timeout time.Duration, maxResponseBytes int64, allowTestEndpoint bool) (*GitHubClient, error) {
	if timeout <= 0 {
		return nil, errors.New("GitHub timeout must be positive")
	}
	if maxResponseBytes <= 0 {
		return nil, errors.New("GitHub max response bytes must be positive")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub API base: %w", err)
	}
	if allowTestEndpoint {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errors.New("test GitHub API base must use http or https")
		}
	} else if parsed.Scheme != "https" || parsed.Hostname() != "api.github.com" {
		return nil, errors.New("public GitHub client is pinned to https://api.github.com")
	}
	if parsed.Host == "" {
		return nil, errors.New("GitHub API base host is required")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &GitHubClient{base: parsed, client: client, maxResponseBytes: maxResponseBytes}, nil
}

func (client *GitHubClient) GetIssue(ctx context.Context, repository string, number int) (GitHubIssue, error) {
	var issue GitHubIssue
	if err := client.getJSON(ctx, repositoryPath(repository, "/issues/"+strconv.Itoa(number)), &issue); err != nil {
		return GitHubIssue{}, err
	}
	return issue, nil
}

func (client *GitHubClient) GetPullRequest(ctx context.Context, repository string, number int) (GitHubPullRequest, error) {
	var pull GitHubPullRequest
	if err := client.getJSON(ctx, repositoryPath(repository, "/pulls/"+strconv.Itoa(number)), &pull); err != nil {
		return GitHubPullRequest{}, err
	}
	return pull, nil
}

func (client *GitHubClient) GetPullRequestFiles(ctx context.Context, repository string, number int) ([]GitHubPRFile, error) {
	var files []GitHubPRFile
	requestPath := repositoryPath(repository, "/pulls/"+strconv.Itoa(number)+"/files?per_page=100")
	if err := client.getJSON(ctx, requestPath, &files); err != nil {
		return nil, err
	}
	if len(files) > maxGitHubPRFiles {
		return nil, fmt.Errorf("pull request files exceed bounded limit %d", maxGitHubPRFiles)
	}
	return files, nil
}

func (client *GitHubClient) GetCheckRuns(ctx context.Context, repository, headSHA string) ([]GitHubCheckRun, error) {
	var response githubCheckRunsResponse
	requestPath := repositoryPath(repository, "/commits/"+url.PathEscape(headSHA)+"/check-runs?per_page=100")
	if err := client.getJSON(ctx, requestPath, &response); err != nil {
		return nil, err
	}
	if response.TotalCount > maxGitHubCheckRuns || len(response.CheckRuns) > maxGitHubCheckRuns {
		return nil, fmt.Errorf("check runs exceed bounded limit %d", maxGitHubCheckRuns)
	}
	return response.CheckRuns, nil
}

func (client *GitHubClient) getJSON(ctx context.Context, requestPath string, output any) error {
	if client == nil || client.base == nil || client.client == nil {
		return errors.New("GitHub client is not initialized")
	}
	target := *client.base
	target.Path = strings.TrimRight(client.base.Path, "/") + pathBeforeQuery(requestPath)
	target.RawQuery = queryPart(requestPath)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "liminal-rail-codex/0.3")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := client.client.Do(request)
	if err != nil {
		return fmt.Errorf("call GitHub API: %w", err)
	}
	defer response.Body.Close()

	body, err := readGitHubBounded(response.Body, client.maxResponseBytes)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &GitHubHTTPError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
			Body:       strings.TrimSpace(string(body)),
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode GitHub API response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("GitHub API response contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing GitHub API response: %w", err)
	}
	return nil
}

func readGitHubBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read GitHub API response: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("GitHub API response exceeds %d bytes", maxBytes)
	}
	return body, nil
}

func repositoryPath(repository, suffix string) string {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return "/repos/invalid/invalid" + suffix
	}
	return "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + suffix
}

func pathBeforeQuery(value string) string {
	if index := strings.IndexByte(value, '?'); index >= 0 {
		return value[:index]
	}
	return value
}

func queryPart(value string) string {
	if index := strings.IndexByte(value, '?'); index >= 0 {
		return value[index+1:]
	}
	return ""
}
