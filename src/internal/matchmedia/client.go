package matchmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	Base    string
	HTTP    *http.Client
	session string
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
		return Status{DisabledReason: "metadata service unavailable"}, fmt.Errorf("matchmedia health %s", resp.Status)
	}
	var body struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return Status{Ready: true, Version: strings.TrimSpace(body.Version)}, nil
}

func (c *Client) Config() (map[string]any, error) {
	var out map[string]any
	if err := c.getJSON("/v1/config", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func (c *Client) SetConfig(patch map[string]any) error {
	if patch == nil {
		patch = map[string]any{}
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.base()+"/v1/config", bytes.NewReader(body))
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
		return fmt.Errorf("set config: %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return waitHealth(c.httpc(), c.Base, 60*time.Second)
}

func (c *Client) postScan(path, mode string, requireEpisodeNFO *bool) (ScanResult, error) {
	payload := map[string]any{"path": path}
	if m := strings.TrimSpace(mode); m != "" {
		payload["mode"] = m
	}
	if requireEpisodeNFO != nil {
		payload["require_episode_nfo"] = *requireEpisodeNFO
	}
	body, err := json.Marshal(payload)
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
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
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

func (c *Client) Ingest(rows []IngestRow) (ScanResult, error) {
	body, err := json.Marshal(rows)
	if err != nil {
		return ScanResult{}, err
	}
	req, err := http.NewRequest(http.MethodPost, c.base()+"/v1/ingest", bytes.NewReader(body))
	if err != nil {
		return ScanResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpc().Do(req)
	if err != nil {
		return ScanResult{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return ScanResult{}, fmt.Errorf("ingest: %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var out ScanResult
	if err := json.Unmarshal(b, &out); err != nil {
		return ScanResult{}, err
	}
	if strings.TrimSpace(out.Session) == "" {
		return ScanResult{}, fmt.Errorf("ingest: missing session")
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
	return Job{}, fmt.Errorf("matchmedia job %s not found", id)
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

func (c *Client) sessionOr(session string) string {
	if strings.TrimSpace(session) != "" {
		return strings.TrimSpace(session)
	}
	if c == nil {
		return ""
	}
	return c.session
}

func (c *Client) Catalog(session, provider, id string) (Catalog, error) {
	session = c.sessionOr(session)
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
	session = c.sessionOr(session)
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

func (c *Client) Run(ctx context.Context, path, mode string, report func(done, total int, name string)) ([]Job, string, error) {
	jobs, err := c.Scan(ctx, path, mode, report)
	if err != nil {
		return nil, c.session, err
	}
	return jobs, c.session, nil
}

// Scan posts a library walk and waits until the stub returns finished jobs.
func (c *Client) Scan(ctx context.Context, path, mode string, report func(done, total int, name string)) ([]Job, error) {
	scanned, err := c.postScan(path, mode, nil)
	if err != nil {
		return nil, err
	}
	c.session = scanned.Session
	return c.waitJobs(ctx, scanned.Session, report)
}

// Match returns the jobs Scan already finished. Stub servers do not match again.
func (c *Client) Match(ctx context.Context, jobs []Job, report func(done, total int, name string), onJob func(Job)) ([]Job, error) {
	return jobs, nil
}

// SelectStored posts the last scan session. Engine ignores that session.
func (c *Client) SelectStored(ctx context.Context, hint Job, provider, id string) (Job, error) {
	return c.Select(c.sessionOr(""), hint.ID, provider, id)
}

func (c *Client) RunIngest(ctx context.Context, rows []IngestRow, report func(done, total int, name string)) ([]Job, string, error) {
	scanned, err := c.Ingest(rows)
	if err != nil {
		return nil, "", err
	}
	c.session = scanned.Session
	jobs, err := c.waitJobs(ctx, scanned.Session, report)
	if err != nil {
		return nil, scanned.Session, err
	}
	return jobs, scanned.Session, nil
}

func (c *Client) Reload() error {
	if c == nil || strings.TrimSpace(c.Base) == "" {
		return nil
	}
	return waitHealth(c.httpc(), c.Base, 5*time.Second)
}

func (c *Client) waitJobs(ctx context.Context, session string, report func(done, total int, name string)) ([]Job, error) {
	deadline := time.Now().Add(75 * time.Minute)
	var last ScanProgress
	var lastJobs []Job
	for {
		if ctx.Err() != nil {
			return lastJobs, ctx.Err()
		}
		if time.Now().After(deadline) {
			return lastJobs, fmt.Errorf("matchmedia timed out")
		}
		p, err := c.ScanStatus(session)
		if err == nil {
			last = p
		}
		jobs, err := c.Jobs(session)
		if err != nil {
			return lastJobs, err
		}
		lastJobs = jobs
		pending := 0
		for _, j := range jobs {
			if j.Status == "pending" || j.Status == "" {
				pending++
			}
		}
		if report != nil {
			report(last.Done, last.Files, "")
		}
		if pending == 0 && !last.Running {
			return lastJobs, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
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
	return fmt.Errorf("matchmedia did not become healthy")
}
