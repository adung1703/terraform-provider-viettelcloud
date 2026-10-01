# Test index — copy a run declaration below and Find Next to jump to its block.
# Chains and runs are listed in file order; setup runs are included.
#
# Chain lookup (shared fixture state):
#   run "querySupportedFilters_isolatedVpcs_matchingObjectsAndConfiguredValuesReturned" {

provider "viettelcloud" {
  endpoint   = var.api_endpoint
  token      = var.token
  project_id = var.project_id
}

# Create one VPC and verify every supported lookup filter.
run "querySupportedFilters_isolatedVpcs_matchingObjectsAndConfiguredValuesReturned" {
  command = apply

  module {
    source = "./fixtures/vpc/datasource"
  }

  variables {
    region           = "vn-central-1"
    vpc1_name        = "tf-test-ds-vpc-1"
    vpc1_cidr        = "10.80.0.0/16"
    vpc1_description = "VPC 1 for data source test"
  }

  # Assert lookup by ID
  assert {
    condition = (
      data.viettelcloud_vpc.by_id.id == viettelcloud_vpc.vpc1.id &&
      data.viettelcloud_vpc.by_id.name == "tf-test-ds-vpc-1" &&
      data.viettelcloud_vpc.by_id.cidr == "10.80.0.0/16" &&
      data.viettelcloud_vpc.by_id.region == "vn-central-1" &&
      data.viettelcloud_vpc.by_id.description == "VPC 1 for data source test" &&
      length(data.viettelcloud_vpc.by_id.display_name) > 0 &&
      length(data.viettelcloud_vpc.by_id.created_at) > 0 &&
      length(data.viettelcloud_vpc.by_id.updated_at) > 0
    )
    error_message = "VPC lookup by ID returned unexpected values."
  }

  # Assert lookup by Name
  assert {
    condition = (
      data.viettelcloud_vpc.by_name.id == viettelcloud_vpc.vpc1.id &&
      data.viettelcloud_vpc.by_name.name == "tf-test-ds-vpc-1" &&
      data.viettelcloud_vpc.by_name.cidr == "10.80.0.0/16" &&
      data.viettelcloud_vpc.by_name.region == "vn-central-1"
    )
    error_message = "VPC lookup by Name returned unexpected values."
  }

  # Assert lookup by CIDR.
  assert {
    condition = (
      data.viettelcloud_vpc.by_cidr.id == viettelcloud_vpc.vpc1.id &&
      data.viettelcloud_vpc.by_cidr.name == "tf-test-ds-vpc-1" &&
      data.viettelcloud_vpc.by_cidr.cidr == "10.80.0.0/16" &&
      data.viettelcloud_vpc.by_cidr.region == "vn-central-1"
    )
    error_message = "VPC lookup by CIDR returned unexpected values."
  }

  # Assert lookup by CIDR and region.
  assert {
    condition = (
      data.viettelcloud_vpc.by_cidr_and_region.id == viettelcloud_vpc.vpc1.id &&
      data.viettelcloud_vpc.by_cidr_and_region.name == "tf-test-ds-vpc-1" &&
      data.viettelcloud_vpc.by_cidr_and_region.cidr == "10.80.0.0/16" &&
      data.viettelcloud_vpc.by_cidr_and_region.region == "vn-central-1"
    )
    error_message = "VPC lookup by CIDR and region returned unexpected values."
  }

  # Assert that multiple filters are combined with AND semantics.
  assert {
    condition = (
      data.viettelcloud_vpc.by_all_filters.id == viettelcloud_vpc.vpc1.id &&
      data.viettelcloud_vpc.by_all_filters.name == "tf-test-ds-vpc-1" &&
      data.viettelcloud_vpc.by_all_filters.cidr == "10.80.0.0/16" &&
      data.viettelcloud_vpc.by_all_filters.region == "vn-central-1"
    )
    error_message = "VPC lookup by combined filters returned unexpected values."
  }
}
