package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hayone1/gitopsify-provider-template/internal/provider"
)

// replay serves testdata/api, like the provider's tests.
func replay(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"message":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", provider.FixtureName(r.URL.RequestURI())))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
}

func yes(string) bool { return true }

func TestRecord(t *testing.T) {
	srv := replay(t)
	t.Cleanup(srv.Close)
	t.Setenv(provider.RecordEnv, "")
	t.Setenv("TEMPLATE_URL", srv.URL)
	out := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(out, "keep.json")
	if err := os.WriteFile(keep, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Missing setting, then refused credentials: testdata stays.
	t.Setenv("TEMPLATE_TOKEN", "")
	if err := run(context.Background(), provider.New("test"), out, false, yes); err == nil {
		t.Error("no token: recorded")
	}
	t.Setenv("TEMPLATE_TOKEN", "wrong")
	if err := run(context.Background(), provider.New("test"), out, false, yes); err == nil {
		t.Error("wrong token: recorded")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("a failed recording changed testdata: %v", err)
	}

	t.Setenv("TEMPLATE_TOKEN", "test-token")
	// Not confirmed: testdata stays.
	if err := run(context.Background(), provider.New("test"), out, false, func(string) bool { return false }); err == nil {
		t.Error("recorded without confirmation")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("an unconfirmed recording changed testdata: %v", err)
	}
	if err := run(context.Background(), provider.New("test"), out, false, yes); err != nil {
		t.Fatal(err)
	}
	got, _ := filepath.Glob(filepath.Join(out, "*.json"))
	want, _ := filepath.Glob("../../testdata/api/*.json")
	if len(got) != len(want) {
		t.Errorf("recorded %d answers, want %d", len(got), len(want))
	}
	if _, err := os.Stat(keep); !os.IsNotExist(err) {
		t.Error("the old testdata wasn't replaced")
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(out), ".record-*"))
	if old, _ := filepath.Glob(out + ".old"); len(old) > 0 {
		leftovers = append(leftovers, old...)
	}
	if len(leftovers) > 0 {
		t.Errorf("temporary directories left: %v", leftovers)
	}
}
