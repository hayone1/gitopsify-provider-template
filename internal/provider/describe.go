// Package provider reads the tool and describes what it finds as resources
// of the tool's Terraform provider (and, through gitopsify, of its Crossplane
// provider).
//
// The example reads a fictional notes app: workspaces (the partitions) hold
// folders, notes and webhooks. Replace it with your tool's API; the shape of
// the code (describe, client, check, discover, one reader per kind) carries
// over.
package provider

import "github.com/hayone1/gitopsify/sdk"

// The provider's names. go run ./tools/prepare sets them.
const (
	// name is the part after gitopsify-provider- in the executable's name.
	name = "template"
	// title is the tool's display name.
	title = "Template"
)

// The upstream providers the output is written for. Pin the versions the
// output was tested with, and refresh testdata/upstream after changing them
// (make upstream).
var (
	terraformProvider = &sdk.TerraformProvider{Name: "notes", Source: "example/notes", Version: "~> 1.4"}

	crossplaneProvider = &sdk.CrossplaneProvider{
		Name:           "provider-notes",
		Package:        "xpkg.example.com/example/provider-notes:v1.4.0",
		ProviderConfig: sdk.GVK{APIVersion: "notes.example.m.crossplane.io/v1beta1", Kind: "ClusterProviderConfig"},
	}
)

// Kind names, as people select them.
const (
	kFolder  = "Folder"
	kNote    = "Note"
	kWebhook = "Webhook"
)

func crossplane(kind string) *sdk.CrossplaneType {
	return &sdk.CrossplaneType{APIVersion: "notes.example.m.crossplane.io/v1alpha1", Kind: kind}
}

// kinds lists what the provider reads, in the order it reads them.
var kinds = []sdk.Kind{
	{Name: kFolder, Plural: "Folders", Description: "folders, nested ones included",
		Terraform: &sdk.TerraformType{Type: "notes_folder"}, Crossplane: crossplane("Folder")},
	{Name: kNote, Plural: "Notes", Description: "notes (their text goes into files)",
		Terraform: &sdk.TerraformType{Type: "notes_note"}, Crossplane: crossplane("Note")},
	// Blocks lists the attributes that are nested blocks in the Terraform
	// schema (sdktest.TerraformBlocks finds them in a schema dump).
	{Name: kWebhook, Plural: "Webhooks", Description: "HTTP webhooks (email webhooks have no Terraform resource and are skipped)",
		Terraform: &sdk.TerraformType{Type: "notes_webhook", Blocks: []string{"retry"}}, Crossplane: crossplane("Webhook")},
}

func description(version string) sdk.Description {
	return sdk.Description{
		APIVersion:  sdk.APIVersion,
		Name:        name,
		Title:       title,
		Version:     version,
		Description: "workspaces, folders, notes and webhooks",
		Homepage:    "https://github.com/hayone1/gitopsify-provider-template",
		Protocols:   []string{sdk.Protocol},
		// Name settings after the upstream Terraform provider's arguments and
		// environment variables, so one environment serves gitopsify and
		// terraform.
		Settings: []sdk.Setting{
			{Name: "url", Title: "URL", Description: "the server's address", Env: "TEMPLATE_URL",
				Required: true, Example: "https://notes.example.com", Terraform: "url", Crossplane: "url"},
			{Name: "token", Title: "API token", Description: "an API token that can read every workspace", Env: "TEMPLATE_TOKEN",
				Secret: true, Required: true, Terraform: "token", Crossplane: "token"},
		},
		Partition: sdk.Noun{Singular: "workspace", Plural: "workspaces",
			Description: "each workspace gets its own Terraform workspace and Kustomization"},
		Kinds:      kinds,
		Terraform:  terraformProvider,
		Crossplane: crossplaneProvider,
	}
}
