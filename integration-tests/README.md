# Terraform integration tests

These tests create real resources on Viettel Cloud using `terraform test`.
Each resource and data source has separate test files and fixtures so they can
be reviewed, run, and updated independently.

## Structure

```text
integration-tests/
├── tests/
│   ├── vpc_resource.tftest.hcl
│   ├── vpc_data_source.tftest.hcl
│   ├── <entity>_resource.tftest.hcl
│   └── <entity>_data_source.tftest.hcl
├── fixtures/
│   ├── vpc/
│   │   ├── resource/main.tf
│   │   └── datasource/main.tf
│   └── <entity>/
│       ├── resource/main.tf
│       └── datasource/main.tf
├── provider.tf
└── terraform.tfvars
```

- `tests/<entity>_<target>.tftest.hcl` contains the lifecycle steps, queries, and
  assertions. `provider.tf` configures the provider from the shared connection
  input variables.
- `fixtures/<entity>/<target>` is an independent Terraform module loaded through
  `module.source` by the test file.
- `fixtures/<entity>/import` is a separate module holding a config-driven
  `import` block, used by the test files that cover an import path. It is
  separate because `terraform test` has no import command and an `import` block
  cannot be switched off by a variable.
- `provider.tf` declares the provider requirement and shared connection input
  variables. A variable that only one test file needs is declared inside that
  `.tftest.hcl` file instead, so the remaining files stay runnable without it.
- `terraform.tfvars` contains connection details and must not be committed.
  `terraform test` auto-loads it for the variables declared in `provider.tf` and
  for those declared inside a test file.
- Runs that load the same fixture within a test file share state. Terraform
  uses this state to update or replace the resource created by an earlier run.
- A run can opt out of that shared state with `state_key`, and runs in different
  state keys can execute at the same time with `parallel = true`. A failing run
  only skips the remaining runs of its own state key.
- Terraform destroys resources in the test state after the test file finishes.

## Test coverage

| Target | Coverage |
| --- | --- |
| Elastic IP Resource | Create and refresh, update/omit/clear description, replace address family, and teardown |
| Elastic IP Data Source | Lookup by ID, IPv4/IPv6 address, status, region, availability, and combined filters; full state mapping and configured-value preservation |
| Key Pair Resource | Create and refresh, generated private-key state, rename and normalization, public-key replacement and omission preservation, plan-only import, and teardown |
| Key Pair Data Source | Lookup by ID, name, fingerprint, and combined filters; full state mapping and configured-value preservation |
| Private IP Resource | Allocated and configured IP/MAC values, allowed VIP lifecycle, description lifecycle, replacement, normalization, and teardown with an attachment |
| Private IP Data Source | Direct ID and representative subnet/VPC name, ID, CIDR, and region selector combinations; full state mapping and configured-value preservation |
| Placement Group Resource | Three parallel chains covering create and refresh, mutable and optional attributes, name and region normalization, policy replacement, plan-only import, and teardown. Region replacement is N/A until the test project provides a second region. |
| Security Group Resource | Create and refresh, description lifecycle, rename in place, omitted-description no-drift, plan-only import, and teardown. Region replacement is N/A until the test project provides a second region. |
| Security Group Data Source | Lookup by ID, name, region, default status, and combined filters; full state mapping and configured-value preservation |
| Security Group Rule Resource | A shared test-owned security group and five parallel rule chains covering create and refresh, update/omit/clear description, backend-assigned optional values, direction/protocol/port-range/remote-prefix replacement, enum normalization, plan-only import, and teardown. security_group_id replacement is N/A because moving a rule requires a second security group. |
| Server Resource | 15 independent parallel chains covering create/refresh, updates, attachments, power, coordinated offline resize/rebuild/start, replacement, flavor and attached-volume resize, import, and teardown |
| Subnet Resource | Create and refresh, update/omit/clear description, normalization, CIDR/VPC replacement, and teardown |
| Subnet Data Source | Lookup by ID, name, CIDR, VPC ID/name, region, and combined filters; full state mapping and configured-value preservation |
| VPC Resource | Create and refresh, update/omit/clear description, CIDR replacement, and teardown |
| VPC Data Source | Lookup by ID, name, CIDR, region, and combined filters; full state mapping and configured-value preservation |
| Volume Resource | Concurrent create and scaling, configured-zone creation, zone omission preservation, snapshot-source zone omission, mutable attributes, normalization, external sources, resize, retype, and stable plans |
| Volume Data Source | Lookup by ID, name, size, status, bootable, zone, and combined filters; full state mapping and configured-value preservation |
| Placement Group Data Source | Create an isolated subject; look up by ID, name, policy, region, and combined filters; verify full state equality and configured-value preservation |

The index and assertions in each `.tftest.hcl` file are the source of truth for
its exact chains, scenarios, inputs, and expected results.

## Prerequisites

The tests require Terraform 1.12 or newer because parallel test chains use
`parallel` and `state_key`. Use credentials for a dedicated test project and a
development override that points to the provider built from this repository.

Each test file declares its environment-specific catalog and resource inputs.
Before running it, ensure that the target project provides the referenced
regions, zones, images, volume types, and flavors, and has no resources that
would collide with the test fixtures. The selected `.tftest.hcl` file is the
source of truth for concrete values and compatibility constraints.

Two test files require additional environment preparation:

- `tests/volume_resource.tftest.hcl`:
  - existing snapshot and backup objects referenced by `snapshot_id` and
    `backup_id`; and
  - quota for up to five volumes totaling at least 150 GiB.
- `tests/server_resource.tftest.hcl`:
  - quota for one VPC, up to 15 servers, and their dependent storage and
    networking resources.

## Set up

Build the provider:

```bash
make build
```

Configure the Terraform CLI using the absolute path to this repository:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/viettelcloud/viettelcloud" = "/absolute/path/to/terraform-provider-viettelcloud"
  }

  direct {}
}
```

Create `integration-tests/terraform.tfvars`:

```hcl
token        = "..."
project_id   = "integration-test-project"
api_endpoint = "<api-endpoint>"
```

Add any test-specific variables declared by the selected `.tftest.hcl` file to
`terraform.tfvars`, or pass them on the command line with `-var`. Terraform
reports `Required variable not set` when a required input is missing.

`integration-tests/terraform.tfvars` is ignored by Git. Do not commit it or
expose its contents.

Set the provider address before running the tests:

```bash
export VIETTELCLOUD_PROVIDER_ADDRESS="registry.terraform.io/viettelcloud/viettelcloud"
```

## Run the tests

Run the following commands from the repository root.

Initialize the integration-test configuration:

```bash
terraform -chdir=integration-tests init -backend=false
```

Run a single test file, for example the VPC resource test:

```bash
terraform -chdir=integration-tests test \
  -filter=tests/vpc_resource.tftest.hcl \
  -verbose
```

Run all integration tests:

```bash
terraform -chdir=integration-tests test -verbose
```

## Interpret the results

When the test succeeds, every run reports `pass` and Terraform finishes with
`Success`:

```text
tests/vpc_resource.tftest.hcl... in progress
  run "createVpc_nameDescriptionCidrAndRegionConfigured_resourceAndDataSourceStateReturned"... pass
  run "changeNameAndDescription_existingVpc_vpcUpdatedInPlace"... pass
  run "omitDescription_descriptionSet_descriptionPreservedInPlace"... pass
  run "clearDescription_descriptionSet_descriptionEmptyInPlace"... pass
  run "changeCidr_existingVpc_vpcReplaced"... pass
tests/vpc_resource.tftest.hcl... tearing down
tests/vpc_resource.tftest.hcl... pass

Success! 5 passed, 0 failed.
```

- Exit code `0` with every run marked `pass` means the test and cleanup
  completed successfully.
- If a run reports `fail`, inspect the `Error` and `error_message` shown below
  that run.
- Runs after a failure can be marked `skip`; this is not a separate failure in
  those runs. In a file with parallel chains, such as the server resource test,
  only the failing run's own chain is skipped; the other chains keep running.
- Parallel runs report as they finish, so the server resource test prints its
  runs interleaved rather than in file order.
- If `tearing down` or cleanup reports an error, the test is not complete even
  if the earlier assertions passed.

These tests use real infrastructure and can consume quota or incur costs.
Terraform applies changes without asking for confirmation. Use a project
dedicated to testing.

If cleanup fails, inspect the test output and manually remove any remaining
resources before running the test again.

## Add a new test

1. Create an independent module at `fixtures/<entity>/<target>`.
2. Create `tests/<entity>_<target>.tftest.hcl` with the required lifecycle or
   lookup runs. Follow the
   [run naming and chain index rules](../CONTRIBUTING.md#real-api-integration-tests).
3. Assert primary attributes, cloud-generated IDs, and platform metadata.
4. Run the file with:
   ```bash
   terraform -chdir=integration-tests test -filter='tests/<entity>_<target>.tftest.hcl' -verbose
   ```
