package provider

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hayone1/gitopsify/sdk"
	"github.com/hayone1/gitopsify/sdk/sdktest"
	"sigs.k8s.io/yaml"
)

// fakeAPI replays the answers in testdata/api (recorded from a real
// instance with make record; see testdata/README.md). overrides replace
// answers by request URI; "HTTP 403" answers with that status.
func fakeAPI(t *testing.T, overrides map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"message":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		if body, ok := overrides[r.URL.RequestURI()]; ok {
			if code, ok := strings.CutPrefix(body, "HTTP "); ok {
				status, _ := strconv.Atoi(code)
				http.Error(w, `{"message":"`+http.StatusText(status)+`"}`, status)
				return
			}
			_, _ = w.Write([]byte(body))
			return
		}
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", FixtureName(r.URL.RequestURI())))
		if err != nil {
			http.Error(w, `{"message":"no fixture for `+r.URL.RequestURI()+`"}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func request(srv *httptest.Server) sdk.Request {
	return sdk.Request{Settings: map[string]string{"url": srv.URL, "token": "test-token"}}
}

func byKey(inv sdk.Inventory) map[string]sdk.Resource {
	m := map[string]sdk.Resource{}
	for _, r := range inv.Resources {
		m[r.Key] = r
	}
	return m
}

// TestConformance checks the description and the inventory against the
// rules gitopsify relies on.
func TestConformance(t *testing.T) {
	sdktest.Conformance(t, New("test"), request(fakeAPI(t, nil)))
}

// TestUpstreamSchemas checks every resource against the upstream Terraform
// schema and Crossplane CRDs in testdata/upstream (make upstream refreshes
// them), so a mapping mistake shows up without running either: unknown or
// read-only attributes, wrong types, sensitive values not given as secrets,
// blocks not listed in TerraformType.Blocks, wrong Crossplane field names.
func TestUpstreamSchemas(t *testing.T) {
	d := sdktest.Describe(t, New("test"))
	inv := sdktest.Discover(t, New("test"), request(fakeAPI(t, nil)))
	schema, err := os.ReadFile("../../testdata/upstream/terraform-schema.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range sdktest.ValidateTerraform(schema, d, inv) {
		t.Errorf("terraform: %s", p)
	}
	files, _ := filepath.Glob("../../testdata/upstream/crds/*.yaml")
	var crds []map[string]any
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var crd map[string]any
		if err := yaml.Unmarshal(b, &crd); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		crds = append(crds, crd)
	}
	for _, p := range sdktest.ValidateCrossplane(crds, d, inv) {
		t.Errorf("crossplane: %s", p)
	}

}

func TestDiscover(t *testing.T) {
	inv := sdktest.Discover(t, New("test"), request(fakeAPI(t, nil)))
	if len(inv.Partitions) != 2 || len(inv.Resources) != 8 || len(inv.Warnings) != 0 {
		t.Fatalf("%d partitions, %d resources, warnings %v", len(inv.Partitions), len(inv.Resources), inv.Warnings)
	}
	res := byKey(inv)

	sub := res["folder/ops/databases"]
	if ref, ok := sub.Attributes["parent_uid"].(sdk.RefValue); !ok || ref.Ref != "folder/ops/runbooks" || ref.Crossplane != "parentRef" || ref.Value != "runbooks" {
		t.Errorf("parent_uid = %#v", sub.Attributes["parent_uid"])
	}
	if sub.ImportID != "ops/databases" || sub.Partition != "ops" {
		t.Errorf("folder: %+v", sub)
	}

	// Files: folder titles as directories, unique per workspace, one segment
	// per title, content kept as is.
	files := map[string]string{}
	for _, k := range []string{"note/ops/welcome", "note/ops/restart-db", "note/ops/restart-db-v1", "note/ops/backups", "note/web/readme"} {
		f, ok := res[k].Attributes["body"].(sdk.FileValue)
		if !ok {
			t.Fatalf("%s body = %#v", k, res[k].Attributes["body"])
		}
		files[k] = f.File
	}
	want := map[string]string{
		"note/ops/welcome":       "notes/unfiled/Welcome.md",
		"note/ops/restart-db":    "notes/Runbooks/Databases/Restart the database.md",
		"note/ops/restart-db-v1": "notes/Runbooks/Databases/Restart the database-restart-db-v1.md",
		"note/ops/backups":       "notes/Runbooks/Backups - restores.md",
		"note/web/readme":        "notes/unfiled/Readme.md",
	}
	for k, w := range want {
		if files[k] != w {
			t.Errorf("%s file = %q, want %q", k, files[k], w)
		}
	}
	welcome := res["note/ops/welcome"].Attributes
	text, _ := sdk.FileText(welcome["body"].(sdk.FileValue).Content)
	if string(text) != "# Welcome\n\nStart with the <b>runbooks</b> & ask in #ops.\n" {
		t.Errorf("body = %q", text)
	}
	if welcome["pinned"] != true || welcome["folder_uid"] != nil {
		t.Errorf("welcome = %#v", welcome)
	}
	if _, ok := res["note/ops/restart-db"].Attributes["pinned"]; ok {
		t.Errorf("pinned=false (the default) is written")
	}

	chat := res["webhook/ops/chat"].Attributes
	if s, ok := chat["url"].(sdk.SecretValue); !ok || s.Value != "https://chat.example.com/hooks/T0K3N" || s.Crossplane != "urlSecretRef" {
		t.Errorf("url = %#v", chat["url"])
	}
	if s, ok := chat["signing_secret"].(sdk.SecretValue); !ok || s.Value != nil {
		t.Errorf("signing_secret = %#v", chat["signing_secret"])
	}
	if j, ok := chat["headers_json"].(sdk.JSONValue); !ok || string(j.JSON.(sdk.RawJSON)) != `{"X-Team":"ops","Content-Type":"application/json"}` {
		t.Errorf("headers_json = %#v (key order must be kept)", chat["headers_json"])
	}
	retry, ok := chat["retry"].([]any)
	if !ok || len(retry) != 1 || retry[0].(map[string]any)["backoff_seconds"] == nil {
		t.Errorf("retry = %#v", chat["retry"])
	}

	if len(inv.Skipped) != 1 || inv.Skipped[0].Name != "Mail" || !strings.Contains(inv.Skipped[0].Reason, "email") {
		t.Errorf("skipped = %+v", inv.Skipped)
	}
}

func TestCheck(t *testing.T) {
	c := sdktest.Check(t, New("test"), request(fakeAPI(t, map[string]string{"/api/workspaces/ops/webhooks": "HTTP 403"})))
	if !strings.HasPrefix(c.Target, "Notes 3.2.1 at http://") || c.Identity != "exporter (admin)" || len(c.Partitions) != 2 {
		t.Errorf("check = %+v", c)
	}
	for _, k := range c.Kinds {
		if k.OK != (k.Kind != kWebhook) {
			t.Errorf("kind %s: ok=%v %s", k.Kind, k.OK, k.Message)
		}
	}
}

func TestScope(t *testing.T) {
	srv := fakeAPI(t, nil)
	req := request(srv)
	req.Scope = sdk.Scope{Partitions: []string{"web"}}
	if inv := sdktest.Discover(t, New("test"), req); len(inv.Partitions) != 1 || len(inv.Resources) != 1 {
		t.Errorf("partition scope: %+v", inv)
	}

	req = request(srv)
	req.Scope = sdk.Scope{Kinds: []string{kNote}}
	for _, r := range sdktest.Discover(t, New("test"), req).Resources {
		if r.Kind != kNote {
			t.Errorf("kind scope: got %s", r.Key)
		}
	}

	// A summary has no attributes but names what each object needs.
	req = request(srv)
	req.Detail = sdk.DetailSummary
	req.Scope = sdk.Scope{Keys: []string{"note/ops/restart-db"}}
	inv := sdktest.Discover(t, New("test"), req)
	if len(inv.Resources) != 1 || inv.Resources[0].Attributes != nil || strings.Join(inv.Resources[0].DependsOn, ",") != "folder/ops/databases" {
		t.Errorf("summary: %+v", inv.Resources)
	}
}

// TestWarnings: what can't be read becomes a warning with its partition and
// kind (gitopsify then keeps that scope's previous export), and the rest is
// still read.
func TestWarnings(t *testing.T) {
	inv := sdktest.Discover(t, New("test"), request(fakeAPI(t, map[string]string{"/api/workspaces/ops/webhooks": "HTTP 403"})))
	if len(inv.Warnings) != 1 || inv.Warnings[0].Partition != "ops" || inv.Warnings[0].Kind != kWebhook || !strings.Contains(inv.Warnings[0].Message, "read access") {
		t.Errorf("warnings = %+v", inv.Warnings)
	}
	if len(inv.Resources) != 7 {
		t.Errorf("%d resources", len(inv.Resources))
	}
}

func TestErrors(t *testing.T) {
	srv := fakeAPI(t, nil)
	for name, tc := range map[string]struct {
		settings map[string]string
		code     string
		hint     string
	}{
		"no url":                 {map[string]string{"token": "x"}, sdk.CodeInvalidSettings, "TEMPLATE_URL"},
		"bad url":                {map[string]string{"url": "notes.example.com", "token": "x"}, sdk.CodeInvalidSettings, ""},
		"no token":               {map[string]string{"url": srv.URL}, sdk.CodeInvalidSettings, "TEMPLATE_TOKEN"},
		"bad token":              {map[string]string{"url": srv.URL, "token": "wrong"}, sdk.CodeUnauthenticated, "TEMPLATE_TOKEN"},
		"credentials in the url": {map[string]string{"url": "https://me:secret@notes.example.com", "token": "x"}, sdk.CodeInvalidSettings, "TEMPLATE_TOKEN"},
	} {
		_, err := sdktest.CheckErr(t, New("test"), sdk.Request{Settings: tc.settings})
		e := sdk.AsError(err)
		if err == nil || e.Code != tc.code || !strings.Contains(e.Hint, tc.hint) || strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: %v (%s)", name, err, e.Code)
		}
	}
}

// TestLinks: the next page may be a full URL of the same server, never of
// another one: the token goes with every request.
func TestLinks(t *testing.T) {
	leaked := 0
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked++
		}
		_, _ = w.Write([]byte(`{"items":[],"next":""}`))
	}))
	t.Cleanup(evil.Close)
	evilHost := strings.TrimPrefix(evil.URL, "http://")
	page := func(id, next string) string {
		return `{"items":[{"id":"` + id + `","title":"` + id + `","body":"x"}],"next":"` + next + `"}`
	}
	for next, ok := range map[string]bool{
		"{server}/api/workspaces/web/notes?page=2": true,
		evil.URL + "/x":          false,
		"//" + evilHost + "/x":   false,
		"@" + evilHost + "/x":    false,
		"http://u:p@" + evilHost: false,
	} {
		overrides := map[string]string{"/api/workspaces/web/notes?page=2": page("two", "")}
		srv := fakeAPI(t, overrides)
		overrides["/api/workspaces/web/notes"] = page("one", strings.ReplaceAll(next, "{server}", srv.URL))
		req := request(srv)
		req.Scope = sdk.Scope{Partitions: []string{"web"}, Kinds: []string{kNote}}
		inv := sdktest.Discover(t, New("test"), req)
		if ok != (len(inv.Warnings) == 0) || ok != (len(inv.Resources) == 2) {
			t.Errorf("next %s: %d resources, warnings %+v", next, len(inv.Resources), inv.Warnings)
		}
	}
	if leaked > 0 {
		t.Errorf("the token went to another server %d times", leaked)
	}
}

// TestRecord: what make record saves is what the tests replay.
func TestRecord(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(RecordEnv, dir)
	sdktest.Discover(t, New("test"), request(fakeAPI(t, nil)))
	got, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	want, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "api", "*.json"))
	if len(got) != len(want) || len(got) == 0 {
		t.Errorf("recorded %d answers, testdata has %d", len(got), len(want))
	}
	for _, w := range want {
		if _, err := os.Stat(filepath.Join(dir, filepath.Base(w))); err != nil {
			t.Errorf("%s wasn't recorded", filepath.Base(w))
		}
	}
}

func TestFixtureName(t *testing.T) {
	if FixtureName("/api/ABC") == FixtureName("/api/abc") {
		t.Error("names differ only in case")
	}
	for _, p := range []string{"/api/x?since=2026-01-01T00:00:00Z", "/api/ünï", "/api/" + strings.Repeat("x", 500)} {
		n := FixtureName(p)
		if strings.ContainsAny(n, `:/\?*"<>| `) || len(n) > 210 || strings.ToLower(n) != n {
			t.Errorf("FixtureName(%q) = %q", p, n)
		}
	}
}

func TestSameServer(t *testing.T) {
	base, _ := url.Parse("https://notes.example.com/sub")
	for link, want := range map[string]bool{
		"https://Notes.Example.com/api":     true,
		"https://notes.example.com:443/api": true,
		"http://notes.example.com/api":      false,
		"https://notes.example.com:8443/x":  false,
		"https://u@notes.example.com/x":     false,
	} {
		u, _ := url.Parse(link)
		if got := sameServer(u, base); got != want {
			t.Errorf("sameServer(%s) = %v", link, got)
		}
	}
}

// TestRecordEscapedIDs: IDs with spaces or non-ASCII are recorded under the
// name the tests replay them from.
func TestRecordEscapedIDs(t *testing.T) {
	empty := `{"items":[],"next":""}`
	srv := fakeAPI(t, map[string]string{
		"/api/workspaces":                     `{"items":[{"id":"my space","name":"Mine"}],"next":""}`,
		"/api/workspaces/my%20space/folders":  empty,
		"/api/workspaces/my%20space/notes":    `{"items":[{"id":"ü","title":"Ü","body":"x"}],"next":""}`,
		"/api/workspaces/my%20space/webhooks": empty,
	})
	dir := t.TempDir()
	t.Setenv(RecordEnv, dir)
	inv := sdktest.Discover(t, New("test"), request(srv))
	if len(inv.Warnings) > 0 || len(inv.Resources) != 1 {
		t.Fatalf("%+v", inv)
	}
	if _, err := os.Stat(filepath.Join(dir, FixtureName("/api/workspaces/my%20space/notes"))); err != nil {
		t.Errorf("recorded under another name: %v", err)
	}
}
