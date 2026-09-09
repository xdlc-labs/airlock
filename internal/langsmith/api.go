package langsmith

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/xdlc-labs/airlock/internal/evalcase"
)

const (
	// DefaultBaseURL is LangSmith's hosted API. Self-hosted deployments serve the
	// same endpoints under a different host and usually an /api/v1 prefix, which
	// belongs in the url the caller passes.
	DefaultBaseURL = "https://api.smith.langchain.com"

	examplePageSize = 100
	maxExamplePages = 200
	maxResponseSize = 32 << 20
)

// Client reads datasets from a LangSmith API. It only ever reads: nothing about
// this repository is sent anywhere except the dataset name being looked up.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient builds a client from baseURL and the environment. An explicit
// baseURL wins over LANGSMITH_ENDPOINT, which wins over LangSmith's own host.
// The key comes from LANGSMITH_API_KEY, or LANGCHAIN_API_KEY for older setups.
func NewClient(baseURL string) (*Client, error) {
	key := cmp.Or(os.Getenv("LANGSMITH_API_KEY"), os.Getenv("LANGCHAIN_API_KEY"))
	if key == "" {
		return nil, errors.New("no LangSmith API key: set LANGSMITH_API_KEY")
	}
	base := cmp.Or(baseURL, os.Getenv("LANGSMITH_ENDPOINT"), DefaultBaseURL)
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		APIKey:  key,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ImportDataset pulls a dataset's examples and converts them to eval cases.
// dataset is a dataset id or its exact name. limit 0 means every example.
func (c *Client) ImportDataset(ctx context.Context, dataset string, limit int) ([]evalcase.Case, error) {
	id, err := c.resolveDataset(ctx, dataset)
	if err != nil {
		return nil, err
	}
	rows, err := c.fetchExamples(ctx, id, limit)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("langsmith dataset %q: no examples", dataset)
	}
	cases := casesFromRows(rows)
	if len(cases) == 0 {
		return nil, fmt.Errorf("langsmith dataset %q: %d examples, none with usable inputs", dataset, len(rows))
	}
	return cases, nil
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// resolveDataset turns a dataset name into its id. An id is passed through, so a
// dataset that happens to be named like a uuid has to be given by id.
func (c *Client) resolveDataset(ctx context.Context, dataset string) (string, error) {
	if dataset == "" {
		return "", errors.New("no dataset given")
	}
	if uuidRE.MatchString(dataset) {
		return dataset, nil
	}
	var found []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	q := url.Values{"name": {dataset}, "limit": {"100"}}
	if err := c.get(ctx, "/datasets", q, &found); err != nil {
		return "", err
	}
	for _, d := range found {
		if d.Name == dataset && d.ID != "" {
			return d.ID, nil
		}
	}
	return "", fmt.Errorf("langsmith dataset %q not found", dataset)
}

// fetchExamples pages through a dataset's examples, oldest page first.
func (c *Client) fetchExamples(ctx context.Context, datasetID string, limit int) ([]map[string]any, error) {
	var out []map[string]any
	for page := 0; page < maxExamplePages; page++ {
		size := examplePageSize
		if limit > 0 && limit-len(out) < size {
			size = limit - len(out)
		}
		if size <= 0 {
			break
		}
		var batch []map[string]any
		q := url.Values{
			"dataset": {datasetID},
			"limit":   {fmt.Sprint(size)},
			"offset":  {fmt.Sprint(len(out))},
		}
		if err := c.get(ctx, "/examples", q, &batch); err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if len(batch) < size {
			return out, nil
		}
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	endpoint := c.BaseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("langsmith %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("langsmith %s: %s (check LANGSMITH_API_KEY)", path, resp.Status)
	case resp.StatusCode >= 300:
		return fmt.Errorf("langsmith %s: %s: %s", path, resp.Status, truncate(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("langsmith %s: %w", path, err)
	}
	return nil
}

func truncate(b []byte) string {
	const limit = 160
	s := strings.TrimSpace(string(b))
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
