# gitopsify-provider-template

<!-- template:start -->
> **This is a template for [gitopsify](https://github.com/hayone1/gitopsify)
> providers.** A provider reads a tool's live state (Keycloak realms, Vault
> policies, ...) so `gitopsify export <name>` can write it as Terraform/OpenTofu
> or Crossplane that adopts what exists without changing it. Like Terraform's
> provider scaffolding, it is a working provider for a made-up tool: rename it,
> then replace the made-up parts with your tool.

## Start a provider

1. Create your repository from this template. Name it
   `gitopsify-provider-<name>`: gitopsify finds providers by that name.

   ```sh
   gh repo create acme/gitopsify-provider-keycloak --template hayone1/gitopsify-provider-template --public --clone
   cd gitopsify-provider-keycloak
   ```

2. Rename everything (module, executable, provider, environment variables).
   This also removes these notes and the rename tool:

   ```sh
   go run ./tools/prepare -name keycloak -title Keycloak
   ```

3. Replace the made-up notes app with your tool in `internal/provider`, one
   file at a time (`AGENTS.md` has the full checklist, for you or a coding
   agent):

   | File | What to change |
   |---|---|
   | `describe.go` | Settings (named after the Terraform provider's arguments and environment variables), the partition (realm, organization...), the kinds and the upstream Terraform and Crossplane providers with their versions |
   | `client.go` | Authentication, and what to tell people when their credentials are refused |
   | `provider.go` | `Check`: what it's connected to, as whom, which partitions it sees |
   | `read.go` | One reader per kind: the tool's objects as resources of the Terraform provider |

4. Test against the real schemas and a real instance:

   ```sh
   make upstream   # the Terraform schema and Crossplane CRDs of those versions, into testdata/upstream
   make record     # your tool's answers, into testdata/api (from a throwaway instance)
   make lint test  # conformance, the mapping checked against the schemas, your assertions
   make install && gitopsify export keycloak   # the real thing
   ```

5. Prove the output on a live instance: `terraform plan` imports only, `apply`,
   `plan` again shows no changes, and the Crossplane resources become Ready and
   Synced without changing the tool. The
   [Grafana provider](https://github.com/hayone1/gitopsify-provider-grafana)'s
   `test/live` does all of it on a throwaway machine.

6. Release: tag `vX.Y.Z` and push the tag. The release workflow publishes the
   archives `gitopsify provider install` expects, with checksums and build
   provenance. Then add your provider to gitopsify's
   [index](https://github.com/hayone1/gitopsify/blob/main/providers/index.yaml).

The template is Apache-2.0 licensed; pick your provider's license (`LICENSE`).
[Writing a provider](https://github.com/hayone1/gitopsify/blob/main/docs/writing-providers.md)
explains the contract, the value markers (`sdk.Ref`, `sdk.Secret`, `sdk.JSON`,
`sdk.File`) and what makes a first plan clean.

### What's in it

| Path | |
|---|---|
| `main.go` | `sdk.Serve`: the protocol, cancellation, panics, logging |
| `internal/provider/` | The example provider for a made-up notes app: workspaces (partitions) of folders, notes and webhooks, shown as references, files, secrets, JSON, a nested block, skipped objects, warnings and paging |
| `internal/provider/provider_test.go` | A fake API from `testdata/api`, `sdktest.Conformance`, validation against `testdata/upstream`, scopes, summaries, errors, links, recording |
| `tools/upstream/` | `make upstream`: the Terraform schema (`terraform providers schema -json`, trimmed) and the CRDs of the Crossplane package |
| `tools/record/` | `make record`: records a real instance's answers, replacing `testdata/api` only when everything was read |
| `tools/prepare/` | The rename tool (removes itself) |
| `.github/workflows/` | CI (lint, test, build) and releases (GoReleaser, attestations) |
| `AGENTS.md` | Instructions for coding agents building the provider |

<!-- template:end -->
<!-- TODO: describe your tool. Everything below still describes the made-up notes app. -->
A [gitopsify](https://github.com/hayone1/gitopsify) provider for Template:
`gitopsify export template` writes its workspaces, folders, notes and webhooks
as Terraform/OpenTofu or Crossplane configuration that adopts the existing
objects without changing them.

## Install

```sh
gitopsify provider install hayone1/template
```

## Use

```sh
export TEMPLATE_URL=https://notes.example.com TEMPLATE_TOKEN=...
gitopsify export template            # a wizard; --help for the flags and CI use
```

| Setting | Environment | |
|---|---|---|
| `url` | `TEMPLATE_URL` | The server's address |
| `token` | `TEMPLATE_TOKEN` | An API token that can read every workspace (never written to Git) |

Each workspace gets its own Terraform workspace or Kustomization.

| Kind | Terraform | Crossplane |
|---|---|---|
| Folder | `notes_folder` | `Folder.notes.example.m.crossplane.io` |
| Note | `notes_note` (text in its own file) | `Note.notes.example.m.crossplane.io` |
| Webhook | `notes_webhook` (URL as a secret; email webhooks are skipped) | `Webhook.notes.example.m.crossplane.io` |

## Development

```sh
make build test lint
make upstream     # after changing the upstream versions or adding a kind
make record       # after changing what the provider reads (a throwaway instance; the settings' environment variables)
make install      # puts gitopsify-provider-template on PATH for gitopsify export
```

See [AGENTS.md](AGENTS.md) and
[Writing a provider](https://github.com/hayone1/gitopsify/blob/main/docs/writing-providers.md).
