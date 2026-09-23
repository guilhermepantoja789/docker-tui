package updatecheck_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guilhermepantoja789/docker-tui/internal/updatecheck"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		remote, local string
		want          bool
	}{
		{"v0.2.0", "0.1.0", true},
		{"0.1.1", "v0.1.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v1.0.0-rc.1", "0.9.0", true},
		{"1.2", "1.1.9", true},
	}
	for _, tc := range cases {
		got, err := updatecheck.IsNewer(tc.remote, tc.local)
		if err != nil {
			t.Fatalf("%s vs %s: %v", tc.remote, tc.local, err)
		}
		if got != tc.want {
			t.Fatalf("%s vs %s: got %v want %v", tc.remote, tc.local, got, tc.want)
		}
	}
}

func TestCheck_FindsNewer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !stringsHasPrefix(r.Header.Get("User-Agent"), "docker-tui/") {
			t.Errorf("User-Agent=%q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.3.0"}`))
	}))
	t.Cleanup(srv.Close)

	prev := updatecheck.LatestURL
	updatecheck.LatestURL = srv.URL
	t.Cleanup(func() { updatecheck.LatestURL = prev })

	res := updatecheck.Check(context.Background(), "0.2.0", srv.Client())
	if res == nil || res.Latest != "v0.3.0" {
		t.Fatalf("got %+v", res)
	}
	if res.Message == "" {
		t.Fatal("expected message")
	}
}

func TestCheck_SkipsWhenCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
	}))
	t.Cleanup(srv.Close)

	prev := updatecheck.LatestURL
	updatecheck.LatestURL = srv.URL
	t.Cleanup(func() { updatecheck.LatestURL = prev })

	if res := updatecheck.Check(context.Background(), "v0.2.0", srv.Client()); res != nil {
		t.Fatalf("expected nil, got %+v", res)
	}
}

func TestCheck_SkipsDevAndEmpty(t *testing.T) {
	if updatecheck.Check(context.Background(), "dev", nil) != nil {
		t.Fatal("expected nil for dev")
	}
	if updatecheck.Check(context.Background(), "", nil) != nil {
		t.Fatal("expected nil for empty")
	}
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
