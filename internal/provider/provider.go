package provider

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/hayone1/gitopsify/sdk"
)

// Provider implements sdk.Provider.
type Provider struct {
	version string
}

// New returns the provider; version is what describe reports.
func New(version string) *Provider { return &Provider{version: version} }

// Describe implements sdk.Provider. It must not connect to anything.
func (p *Provider) Describe() sdk.Description { return description(p.version) }

type info struct {
	Product string `json:"product"`
	Version string `json:"version"`
}

type whoami struct {
	Login string `json:"login"`
	Role  string `json:"role"`
}

type workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// session is what check and discover learn when they connect.
type session struct {
	c          *client
	info       info
	who        whoami
	workspaces []workspace
}

func connect(ctx context.Context, req sdk.Request) (*session, error) {
	c, err := newClient(req.Settings)
	if err != nil {
		return nil, err
	}
	s := &session{c: c}
	if err := c.get(ctx, "/api/info", &s.info); err != nil {
		return nil, err
	}
	if err := c.get(ctx, "/api/whoami", &s.who); err != nil {
		return nil, err
	}
	if s.workspaces, err = list[workspace](ctx, c, "/api/workspaces"); err != nil {
		return nil, err
	}
	return s, nil
}

func partition(w workspace) sdk.Partition { return sdk.Partition{ID: w.ID, Name: w.Name} }

// Check implements sdk.Provider: what the settings connect to, as whom,
// which partitions that identity sees and which kinds it can read.
func (p *Provider) Check(ctx context.Context, req sdk.Request) (*sdk.CheckResult, error) {
	s, err := connect(ctx, req)
	if err != nil {
		return nil, err
	}
	res := &sdk.CheckResult{
		Target:   fmt.Sprintf("%s %s at %s", s.info.Product, s.info.Version, s.c.base),
		Identity: fmt.Sprintf("%s (%s)", s.who.Login, s.who.Role),
	}
	for _, w := range s.workspaces {
		res.Partitions = append(res.Partitions, partition(w))
	}
	if len(s.workspaces) == 0 {
		res.Warnings = append(res.Warnings, "the token sees no workspaces")
		return res, nil
	}
	// Probe each kind in the first workspace, so people learn about a missing
	// permission before they pick kinds.
	first := s.workspaces[0].ID
	for _, rd := range readers {
		kc := sdk.KindCheck{Kind: rd.kind, OK: true}
		if err := s.c.get(ctx, workspacePath(first, rd.path), nil); err != nil {
			kc.OK, kc.Message = false, sdk.AsError(err).Error()
		}
		res.Kinds = append(res.Kinds, kc)
	}
	return res, nil
}

// reader reads one kind in one workspace.
type reader struct {
	kind string
	// path is the kind's list in a workspace, which check probes.
	path string
	read func(ctx context.Context, r *run, w workspace) error
}

// readers lists one reader per kind. Folders come first: notes need them for
// their file paths.
var readers = []reader{
	{kFolder, "folders", readFolders},
	{kNote, "notes", readNotes},
	{kWebhook, "webhooks", readWebhooks},
}

// workspacePath is a list in a workspace. IDs are escaped: they come from
// the tool.
func workspacePath(id, rest string) string {
	return "/api/workspaces/" + url.PathEscape(id) + "/" + rest
}

// run is one discover call.
type run struct {
	s   *session
	req sdk.Request
	out sdk.Emitter
	// folders holds each workspace's folders by ID (for file paths), loaded
	// once even when folders themselves aren't exported.
	folders map[string]map[string]folder
	// files holds the file paths used so far, by workspace: two notes with
	// the same title in the same folder must not share a file.
	files map[string]map[string]bool
}

// Discover implements sdk.Provider: it sends each partition in scope, then
// its resources, as it reads them.
func (p *Provider) Discover(ctx context.Context, req sdk.Request, out sdk.Emitter) error {
	s, err := connect(ctx, req)
	if err != nil {
		return err
	}
	r := &run{s: s, req: req, out: out, folders: map[string]map[string]folder{}, files: map[string]map[string]bool{}}
	log := sdk.Logger(ctx)
	for _, w := range s.workspaces {
		if !req.Scope.HasPartition(w.ID) {
			continue
		}
		if err := out.Partition(partition(w)); err != nil {
			return err
		}
		log.Info(fmt.Sprintf("reading workspace %q", w.Name))
		for _, rd := range readers {
			if !req.Scope.HasKind(rd.kind) {
				continue
			}
			if err := rd.read(ctx, r, w); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// Something that can't be read is a warning, not an error. With
				// the partition and the kind set, gitopsify keeps the previous
				// export of that scope instead of treating it as deleted.
				if err := r.warn(w, rd.kind, err); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *run) warn(w workspace, kind string, err error) error {
	return r.out.Warn(sdk.Warning{Partition: w.ID, Kind: kind, Message: sdk.AsError(err).Error()})
}

// emit sends a resource unless the scope leaves it out. A summary (people
// picking objects) has no attributes but keeps the references.
func (r *run) emit(res sdk.Resource) error {
	if !r.req.Scope.HasKey(res.Key) {
		return nil
	}
	if r.req.Detail == sdk.DetailSummary {
		res = res.Summary()
	}
	return r.out.Resource(res)
}

// skip reports an object left out on purpose, and why.
func (r *run) skip(w workspace, key, kind, name, reason string) error {
	if !r.req.Scope.HasKey(key) {
		return nil
	}
	return r.out.Skip(sdk.Skipped{Kind: kind, Partition: w.ID, Name: name, Reason: reason})
}

// key identifies a resource within the instance, stably across runs:
// references and re-exports rely on it. Use IDs, never display names.
func key(kind string, w workspace, id string) string {
	return strings.ToLower(kind) + "/" + w.ID + "/" + id
}

// importID is what `terraform import` takes for the upstream resource.
func importID(w workspace, id string) string { return w.ID + "/" + id }

// file returns a unique file path in the workspace built from display names.
// gitopsify makes every segment safe for every OS; the provider only keeps
// paths clean and unique. Call it in a stable order (sorted by ID) so the
// same object gets the same path in every export.
func (r *run) file(w workspace, id, ext string, names ...string) string {
	segs := make([]string, len(names))
	for i, n := range names {
		segs[i] = segment(n)
	}
	used := r.files[w.ID]
	if used == nil {
		used = map[string]bool{}
		r.files[w.ID] = used
	}
	p := path.Join(segs...) + ext
	last := segs[len(segs)-1]
	for n := 1; used[p]; n++ {
		segs[len(segs)-1] = last + "-" + segment(id)
		if n > 1 {
			segs[len(segs)-1] += "-" + strconv.Itoa(n)
		}
		p = path.Join(segs...) + ext
	}
	used[p] = true
	return p
}

// segment makes a display name one path segment.
func segment(s string) string {
	s = strings.NewReplacer("/", "-", `\`, "-").Replace(strings.TrimSpace(s))
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}
