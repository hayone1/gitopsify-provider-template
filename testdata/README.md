# Test data

Until you replace them, these are the made-up notes app's.

- `api/`: the tool's answers, replayed by the unit tests (`fakeAPI` in
  `internal/provider/provider_test.go`), one file per request, named after
  its escaped path and query (`provider.FixtureName`). Record them from a
  throwaway instance after changing what the provider reads:

  ```sh
  TEMPLATE_URL=http://localhost:8080 TEMPLATE_TOKEN=... make record
  ```

  Fill that instance with test objects that cover every kind and case first
  (a script, like the Grafana provider's `test/live/seed.sh`, makes it
  repeatable). Only record a throwaway instance: answers can hold secrets.

- `upstream/terraform-schema.json`: `terraform providers schema -json` for
  the Terraform provider in `internal/provider/describe.go`, trimmed to the
  resource types the kinds use.
- `upstream/crds/`: the CRDs of the Crossplane package for those kinds.
- `upstream/VERSIONS`: the versions they were taken from.

  `make upstream` refreshes all three. The unit tests check every resource
  against them (`sdktest.ValidateTerraform`, `sdktest.ValidateCrossplane`), so
  a mapping mistake shows up without running Terraform or Crossplane.
