# Agent instructions

This repository is a [gitopsify](https://github.com/hayone1/gitopsify)
provider: an executable, `gitopsify-provider-template`, that reads a tool's
live state and describes it as resources of the tool's Terraform provider.
gitopsify turns that into Terraform/OpenTofu or Crossplane that adopts the
existing objects. Read
[Writing a provider](https://github.com/hayone1/gitopsify/blob/main/docs/writing-providers.md)
before changing the mapping.

<!-- template:start -->
## This is still the template

Run `go run ./tools/prepare -name <name> -title <Title>` first (see
README.md); it renames everything and removes this section. Then work
through "Replacing the example" below.

<!-- template:end -->
## Replacing the example

The code started from a template that reads a made-up notes app. Until this
list is done, delete nothing of it you haven't replaced, and delete this
section when it is.

- [ ] `internal/provider/describe.go`: title, description, homepage;
  settings named after the upstream Terraform provider's arguments and
  environment variables; the partition; the kinds with their Terraform
  types, blocks and Crossplane GVKs; the upstream Terraform and Crossplane
  providers and versions.
- [ ] `client.go`: authentication and its error hints, paging. Keep the
  same-server check on links, the size limit and the record mode.
- [ ] `provider.go`: `Check` (target, identity, partitions, a probe per
  kind).
- [ ] `read.go`: one reader per kind; the notes readers removed.
- [ ] `make upstream` (replaces the made-up `testdata/upstream`) and
  `make record` against a throwaway instance (replaces `testdata/api`);
  `testdata/README.md` describes how to fill that instance.
- [ ] `provider_test.go`: assertions about your tool's objects instead of
  notes (keep the conformance, upstream, scope, error, link and record
  tests); `tools/record/main_test.go`: its fake server's auth and the
  settings' environment variables.
- [ ] `README.md`: what it reads, its settings, the kinds table; the TODO
  comment removed.
- [ ] A live run: `terraform plan` imports only, `apply`, `plan` shows no
  changes; Crossplane resources Ready and Synced without changing the tool.

## Layout

- `internal/provider/describe.go`: settings, partition, kinds, upstream
  Terraform and Crossplane providers.
- `client.go`: HTTP, errors with codes and hints, paging, recording.
- `provider.go`: `Check`, `Discover`, scope and summary handling.
- `read.go`: one reader per kind.
- `provider_test.go` replays `testdata/api` and validates against
  `testdata/upstream`.
- `tools/upstream`, `tools/record`: `make upstream`, `make record`.

## Commands

- `make lint test` before every commit.
- `make upstream` after changing an upstream version or adding a kind (needs
  terraform, or TF=tofu).
- `make record` after changing what the provider reads: needs a throwaway
  instance and its settings in the environment variables `describe.go`
  names. It changes `testdata/api` only when everything could be read.

## Rules

- **Keys** (`Resource.Key`) are stable IDs, never display names: references
  and safe re-exports depend on them.
- **Attributes** use the Terraform schema's names and types. Emit what differs
  from the defaults; leave out IDs and computed values the tool assigns. The
  first `terraform plan` must only import.
- **Markers**: another object's ID is `sdk.Ref`, every attribute the schema
  marks sensitive is `sdk.Secret` (value nil when the API doesn't return it),
  a JSON string is `sdk.JSON` (`sdk.RawJSON` keeps key order), a big document
  is `sdk.File` with a clean relative path that is unique in the partition and
  stable across runs (assign paths in ID order).
- **Never emit part of an object** whose missing part the upstream provider
  would delete; skip the whole object with `out.Skip` and say why.
- **Warnings**: when something can't be read, `out.Warn` with the partition
  and the kind set, and carry on. Errors are for what stops everything (bad
  settings, refused credentials); return them as `sdk.Errorf(code, ...)`
  with a hint that says what to do. Never put credentials in messages.
- Honour `req.Scope` and `req.Detail == sdk.DetailSummary`
  (`Resource.Summary()`).
- Blocks: every attribute written as a nested block must be listed in the
  kind's `TerraformType.Blocks`, and every listed path must be a block in
  the schema (`sdk/sdktest.ValidateTerraform` checks both).
- Recorded answers can hold secrets: record only throwaway instances, never
  production, and never commit credentials.
- Prove a mapping change live before releasing it.
