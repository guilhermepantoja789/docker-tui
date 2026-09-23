// Package updatecheck queries GitHub Releases for a newer docker-tui version.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	repoOwner = "guilhermepantoja789"
	repoName  = "docker-tui"
	defaultAPIURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases/latest"
)

// LatestURL is the GitHub releases/latest endpoint (overridable in tests).
var LatestURL = defaultAPIURL

// Result is a non-blocking update notice for the UI.
type Result struct {
	Latest  string
	Message string
}

type releaseResp struct {
	TagName string `json:"tag_name"`
}

// Check compares current against the latest GitHub release.
// Returns nil when no update is available, current is "dev", or the check fails.
func Check(ctx context.Context, current string, client *http.Client) *Result {
	current = strings.TrimSpace(current)
	if current == "" || current == "dev" {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "docker-tui/"+current)

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var body releaseResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil
	}
	latest := strings.TrimSpace(body.TagName)
	if latest == "" {
		return nil
	}
	newer, err := IsNewer(latest, current)
	if err != nil || !newer {
		return nil
	}
	return &Result{
		Latest: latest,
		Message: fmt.Sprintf(
			"update available: %s — curl -fsSL https://raw.githubusercontent.com/%s/%s/main/scripts/install.sh | bash",
			latest, repoOwner, repoName,
		),
	}
}

// IsNewer reports whether remote is a higher semver than local (v-prefix optional).
func IsNewer(remote, local string) (bool, error) {
	r, err := parseSemver(remote)
	if err != nil {
		return false, err
	}
	l, err := parseSemver(local)
	if err != nil {
		return false, err
	}
	for i := 0; i < 3; i++ {
		if r[i] > l[i] {
			return true, nil
		}
		if r[i] < l[i] {
			return false, nil
		}
	}
	return false, nil
}

func parseSemver(s string) ([3]int, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return [3]int{}, fmt.Errorf("invalid semver %q", s)
	}
	var out [3]int
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return [3]int{}, fmt.Errorf("invalid semver %q", s)
		}
		out[i] = n
	}
	return out, nil
}
