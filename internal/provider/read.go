package provider

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/hayone1/gitopsify/sdk"
)

// One reader per kind. Each turns the tool's objects into resources of the
// upstream Terraform provider:
//
//   - Attributes use the Terraform schema's names and types. Emit what
//     differs from the schema's defaults, leave out what the tool assigns or
//     computes, so the first plan only imports.
//   - Another object's ID is an sdk.Ref, every attribute the schema marks
//     sensitive an sdk.Secret, a string holding JSON an sdk.JSON and a big
//     document an sdk.File.
//   - Never emit part of an object whose missing part the upstream provider
//     would delete: skip the whole object and say why.

// --- folders ------------------------------------------------------------

type folder struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ParentID string `json:"parentId"`
}

func (r *run) loadFolders(ctx context.Context, w workspace) (map[string]folder, error) {
	if m, ok := r.folders[w.ID]; ok {
		return m, nil
	}
	all, err := list[folder](ctx, r.s.c, workspacePath(w.ID, "folders"))
	if err != nil {
		return nil, err
	}
	m := make(map[string]folder, len(all))
	for _, f := range all {
		m[f.ID] = f
	}
	r.folders[w.ID] = m
	return m, nil
}

// folderPath is the titles from the top folder down, for file paths.
func (r *run) folderPath(w workspace, id string) []string {
	var titles []string
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		f, ok := r.folders[w.ID][id]
		if !ok {
			break
		}
		titles = append([]string{f.Title}, titles...)
		id = f.ParentID
	}
	return titles
}

// folderRef refers to a folder's uid: in Terraform an expression when both
// end up in the same workspace, in Crossplane the folderRef/parentRef field;
// otherwise the literal ID.
func folderRef(w workspace, id, crossplaneField string) sdk.RefValue {
	return sdk.Ref(key(kFolder, w, id), "uid", id, crossplaneField)
}

func readFolders(ctx context.Context, r *run, w workspace) error {
	m, err := r.loadFolders(ctx, w)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f := m[id]
		attrs := map[string]any{"workspace": w.ID, "uid": f.ID, "title": f.Title}
		if f.ParentID != "" {
			attrs["parent_uid"] = folderRef(w, f.ParentID, "parentRef")
		}
		if err := r.emit(sdk.Resource{
			Key: key(kFolder, w, f.ID), Kind: kFolder, Partition: w.ID, Name: f.Title,
			ImportID: importID(w, f.ID), Attributes: attrs,
		}); err != nil {
			return err
		}
	}
	return nil
}

// --- notes --------------------------------------------------------------

type note struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	FolderID string   `json:"folderId"`
	Body     string   `json:"body"`
	Tags     []string `json:"tags"`
	Pinned   bool     `json:"pinned"`
}

func readNotes(ctx context.Context, r *run, w workspace) error {
	if _, err := r.loadFolders(ctx, w); err != nil {
		return err
	}
	notes, err := list[note](ctx, r.s.c, workspacePath(w.ID, "notes"))
	if err != nil {
		return err
	}
	// Sorted, so notes with the same title keep their file paths whatever
	// order the API returns them in.
	sort.Slice(notes, func(i, j int) bool { return notes[i].ID < notes[j].ID })
	for _, n := range notes {
		dir := r.folderPath(w, n.FolderID)
		if len(dir) == 0 {
			dir = []string{"unfiled"}
		}
		names := append(append([]string{"notes"}, dir...), n.Title)
		attrs := map[string]any{
			"workspace": w.ID, "uid": n.ID, "title": n.Title,
			// The text goes into its own file, which Terraform reads with
			// file() and Crossplane gets inline.
			"body": sdk.File(r.file(w, n.ID, ".md", names...), n.Body),
		}
		if n.FolderID != "" {
			attrs["folder_uid"] = folderRef(w, n.FolderID, "folderRef")
		}
		if len(n.Tags) > 0 {
			attrs["tags"] = n.Tags
		}
		if n.Pinned {
			attrs["pinned"] = true
		}
		if err := r.emit(sdk.Resource{
			Key: key(kNote, w, n.ID), Kind: kNote, Partition: w.ID, Name: n.Title,
			ImportID: importID(w, n.ID), Attributes: attrs,
		}); err != nil {
			return err
		}
	}
	return nil
}

// --- webhooks -----------------------------------------------------------

type webhook struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	URL     string          `json:"url"`
	Events  []string        `json:"events"`
	Headers json.RawMessage `json:"headers"`
	// Signed webhooks have a signing secret, which the API never returns.
	Signed bool `json:"signed"`
	Retry  *struct {
		Attempts       int `json:"attempts"`
		BackoffSeconds int `json:"backoffSeconds"`
	} `json:"retry"`
}

func readWebhooks(ctx context.Context, r *run, w workspace) error {
	hooks, err := list[webhook](ctx, r.s.c, workspacePath(w.ID, "webhooks"))
	if err != nil {
		return err
	}
	for _, h := range hooks {
		k := key(kWebhook, w, h.ID)
		if h.Type != "http" {
			if err := r.skip(w, k, kWebhook, h.Name, "the Terraform provider only manages HTTP webhooks, not "+h.Type+" ones"); err != nil {
				return err
			}
			continue
		}
		attrs := map[string]any{
			"workspace": w.ID, "uid": h.ID, "name": h.Name, "events": h.Events,
			// The URL holds a token: sensitive in the schema, so a Secret. It
			// goes to git-ignored files and Kubernetes Secrets, never to Git.
			"url": sdk.Secret("url", h.URL, "urlSecretRef", "the webhook's URL"),
		}
		if h.Signed {
			// nil: the tool doesn't return it. gitopsify leaves it out and
			// says so in the report.
			attrs["signing_secret"] = sdk.Secret("signing_secret", nil, "signingSecretSecretRef", "the webhook's signing secret")
		}
		if len(h.Headers) > 0 && string(h.Headers) != "null" && string(h.Headers) != "{}" {
			// RawJSON keeps the tool's key order.
			attrs["headers_json"] = sdk.JSON(sdk.RawJSON(h.Headers))
		}
		if h.Retry != nil {
			// A nested block: a list of objects, listed in TerraformType.Blocks.
			attrs["retry"] = []any{map[string]any{"attempts": h.Retry.Attempts, "backoff_seconds": h.Retry.BackoffSeconds}}
		}
		if err := r.emit(sdk.Resource{
			Key: k, Kind: kWebhook, Partition: w.ID, Name: h.Name,
			ImportID: importID(w, h.ID), Attributes: attrs,
		}); err != nil {
			return err
		}
	}
	return nil
}
