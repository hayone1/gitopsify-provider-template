module github.com/hayone1/gitopsify-provider-template

go 1.23

require (
	github.com/hayone1/gitopsify/sdk v0.0.0-00010101000000-000000000000
	sigs.k8s.io/yaml v1.6.0
)

require go.yaml.in/yaml/v2 v2.4.2 // indirect

// Until github.com/hayone1/gitopsify/sdk has a tagged release, build against
// a sibling checkout of gitopsify. Drop this line and require a tagged
// sdk/vX.Y.Z (go get github.com/hayone1/gitopsify/sdk@latest) before you
// release: go install refuses modules with replace directives.
replace github.com/hayone1/gitopsify/sdk => ../gitopsify/sdk
