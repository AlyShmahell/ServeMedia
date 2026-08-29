package matchora

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Status struct {
	Ready          bool
	DisabledReason string
	Hint           string
}

type Client struct {
	Base string
	HTTP *http.Client
}

type ScanResult struct {
	Session string `json:"session"`
	Files   int    `json:"files"`
}

type ScanProgress struct {
	Files   int  `json:"files"`
	Done    int  `json:"done"`
	Chunks  int  `json:"chunks"`
	Chunk   int  `json:"chunk"`
	Running bool `json:"running"`
}

type JobFile struct {
	Path    string `json:"path"`
	Season  string `json:"season,omitempty"`
	Episode string `json:"episode,omitempty"`
}

type Job struct {
	ID         string      `json:"id"`
	Source     string      `json:"source"`
	Title      string      `json:"title"`
	Year       string      `json:"year,omitempty"`
	Path       string      `json:"path,omitempty"`
	Files      []JobFile   `json:"files,omitempty"`
	Status     string      `json:"status"`
	Error      string      `json:"error,omitempty"`
	Match      *Candidate  `json:"match,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Catalog    []Season    `json:"catalog,omitempty"`
}

type Candidate struct {
	Provider string  `json:"provider"`
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Year     string  `json:"year,omitempty"`
	Score    float64 `json:"score,omitempty"`
	Synopsis string  `json:"synopsis,omitempty"`
	Poster   string  `json:"poster,omitempty"`
}

type Catalog struct {
	Provider string   `json:"provider"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Year     string   `json:"year,omitempty"`
	Type     string   `json:"type"`
	Synopsis string   `json:"synopsis,omitempty"`
	Poster   string   `json:"poster,omitempty"`
	Seasons  []Season `json:"seasons,omitempty"`
}

type Season struct {
	Number   string    `json:"number,omitempty"`
	Title    string    `json:"title"`
	Synopsis string    `json:"synopsis,omitempty"`
	Poster   string    `json:"poster,omitempty"`
	Year     string    `json:"year,omitempty"`
	Episodes []Episode `json:"episodes,omitempty"`
}

type Episode struct {
	Number   string   `json:"number,omitempty"`
	Title    string   `json:"title"`
	Synopsis string   `json:"synopsis,omitempty"`
	Poster   string   `json:"poster,omitempty"`
	Year     string   `json:"year,omitempty"`
	Path     string   `json:"path,omitempty"`
	Paths    []string `json:"paths,omitempty"`
}

func withSession(path, session string) string {
	session = strings.TrimSpace(session)
	if session == "" {
		return path
	}
	u, err := url.Parse(path)
	if err != nil {
		return path
	}
	q := u.Query()
	q.Set("session", session)
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) httpc() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) base() string {
	return strings.TrimRight(c.Base, "/")
}

func (c *Client) Status() (Status, error) {
	if c == nil || strings.TrimSpace(c.Base) == "" {
		return Status{DisabledReason: "metadata service unavailable"}, fmt.Errorf("metadata service unavailable")
	}
	req, err := http.NewRequest(http.MethodGet, c.base()+"/health", nil)
	if err != nil {
		return Status{}, err
	}
	resp, err := c.httpc().Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Status{DisabledReason: "metadata service unavailable"}, fmt.Errorf("matchora health %s", resp.Status)
	}
	return Status{Ready: true}, nil
}

func (c *Client) Scan(path string) (ScanResult, error) {
	body, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return ScanResult{}, err
	}
	req, err := http.NewRequest(http.MethodPost, c.base()+"/v1/scan", bytes.NewReader(body))
	if err != nil {
		return ScanResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpc().Do(req)
	if err != nil {
		return ScanResult{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return ScanResult{}, fmt.Errorf("scan: %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var out ScanResult
	if err := json.Unmarshal(b, &out); err != nil {
		return ScanResult{}, err
	}
	if strings.TrimSpace(out.Session) == "" {
		return ScanResult{}, fmt.Errorf("scan: missing session")
	}
	return out, nil
}

func (c *Client) ScanStatus(session string) (ScanProgress, error) {
	var p ScanProgress
	err := c.getJSON(withSession("/v1/scan/status", session), &p)
	return p, err
}

func (c *Client) Jobs(session string) ([]Job, error) {
	var list []Job
	if err := c.getJSON(withSession("/v1/jobs", session), &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (c *Client) Job(session, id string) (Job, error) {
	list, err := c.Jobs(session)
	if err != nil {
		return Job{}, err
	}
	for _, j := range list {
		if j.ID == id {
			return j, nil
		}
	}
	return Job{}, fmt.Errorf("matchora job %s not found", id)
}

func (c *Client) Select(session, jobID, provider, id string) (Job, error) {
	body, err := json.Marshal(map[string]string{"provider": provider, "id": id})
	if err != nil {
		return Job{}, err
	}
	req, err := http.NewRequest(http.MethodPost, c.base()+withSession("/v1/jobs/"+url.PathEscape(jobID)+"/select", session), bytes.NewReader(body))
	if err != nil {
		return Job{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpc().Do(req)
	if err != nil {
		return Job{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return Job{}, fmt.Errorf("select: %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var j Job
	if err := json.Unmarshal(b, &j); err != nil {
		return Job{}, err
	}
	return j, nil
}

func (c *Client) Catalog(session, provider, id string) (Catalog, error) {
	var t Catalog
	path := withSession("/v1/catalog/"+url.PathEscape(provider)+"/"+url.PathEscape(id), session)
	if err := c.getJSON(path, &t); err != nil {
		return t, err
	}
	return t, nil
}

func (c *Client) ResolveURL(u, session string) string {
	u = strings.TrimSpace(u)
	if u == "" || c == nil {
		return u
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	path := u
	if !strings.HasPrefix(u, "/") {
		path = "/" + u
	}
	resolved := c.base() + path
	if session != "" && strings.Contains(path, "/v1/catalog/") {
		return withSession(resolved, session)
	}
	return resolved
}

func (c *Client) DownloadURL(u, session string) ([]byte, string, error) {
	u = c.ResolveURL(u, session)
	if u == "" {
		return nil, "", fmt.Errorf("empty image URL")
	}
	resp, err := c.httpc().Get(u)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("image download %s", resp.Status)
	}
	ext := ".jpg"
	ct := resp.Header.Get("Content-Type")
	switch {
	case strings.Contains(ct, "png"):
		ext = ".png"
	case strings.Contains(ct, "webp"):
		ext = ".webp"
	}
	return b, ext, nil
}

func (c *Client) SecretsStatus() (map[string]bool, error) {
	var out map[string]bool
	if err := c.getJSON("/v1/secrets", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SetSecrets(updates map[string]string) error {
	body, err := json.Marshal(updates)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.base()+"/v1/secrets", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpc().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("set secrets: %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return waitHealth(c.httpc(), c.Base, 60*time.Second)
}

func (c *Client) getJSON(path string, dest any) error {
	req, err := http.NewRequest(http.MethodGet, c.base()+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpc().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%s: %s %s", path, resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func (t Catalog) FindSeason(n int) *Season {
	for i := range t.Seasons {
		if atoi(t.Seasons[i].Number) == n {
			return &t.Seasons[i]
		}
	}
	return nil
}

func (t Catalog) FindEpisode(season, episode int) *Episode {
	s := t.FindSeason(season)
	if s == nil {
		return nil
	}
	for i := range s.Episodes {
		if atoi(s.Episodes[i].Number) == episode {
			return &s.Episodes[i]
		}
	}
	return nil
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	if len(s) >= 4 {
		if n, err := strconv.Atoi(s[:4]); err == nil {
			return n
		}
	}
	n, _ := strconv.Atoi(s)
	return n
}

func waitHealth(hc *http.Client, base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := strings.TrimRight(base, "/") + "/health"
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req = req.WithContext(ctx)
		resp, err := hc.Do(req)
		cancel()
		if err != nil || resp.StatusCode >= 300 {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		resp.Body.Close()
		return nil
	}
	return fmt.Errorf("matchora did not become healthy")
}
