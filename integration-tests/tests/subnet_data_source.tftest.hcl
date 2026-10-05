# Test index — copy a run declaration below and Find Next to jump to its block.
# Chains and runs are listed in file order; setup runs are included.
#
# Chain lookup (shared fixture state):
#   run "querySupportedFilters_isolatedSubnets_matchingObjectsAndConfiguredValuesReturned" {

provider "viettelcloud" {
  endpoint   = var.api_endpoint
  token      = var.token
  project_id = var.project_id
}

# Create one isolated subnet and verify every supported lookup criterion.
run "querySupportedFilters_isolatedSubnets_matchingObjectsAndConfiguredValuesReturned" {
  command = apply

  module {
    source = "./fixtures/subnet/datasource"
  }

  variables {
    region             = "vn-central-1"
    vpc_name           = "tf-test-subnet-ds-vpc"
    vpc_cidr           = "10.90.0.0/16"
    subnet_name        = "tf-test-subnet-ds-primary"
    subnet_description = "Subnet for data source integration tests"
    subnet_cidr        = "10.90.1.0/24"
  }

  # ID uses the direct Get path and returns every result attribute.
  assert {
    condition = (
      data.viettelcloud_subnet.by_id.id == viettelcloud_subnet.subnet.id &&
      data.viettelcloud_subnet.by_id.name == "tf-test-subnet-ds-primary" &&
      data.viettelcloud_subnet.by_id.description == "Subnet for data source integration tests" &&
      data.viettelcloud_subnet.by_id.cidr == "10.90.1.0/24" &&
      data.viettelcloud_subnet.by_id.vpc_id == viettelcloud_vpc.vpc.id &&
      data.viettelcloud_subnet.by_id.vpc_name == "tf-test-subnet-ds-vpc" &&
      data.viettelcloud_subnet.by_id.region == "vn-central-1" &&
      length(data.viettelcloud_subnet.by_id.display_name) > 0 &&
      length(data.viettelcloud_subnet.by_id.created_at) > 0 &&
      length(data.viettelcloud_subnet.by_id.updated_at) > 0
    )
    error_message = "Subnet lookup by ID returned incomplete or unexpected state."
  }

  assert {
    condition     = data.viettelcloud_subnet.by_name.id == viettelcloud_subnet.subnet.id
    error_message = "Subnet lookup by name did not return the expected subnet."
  }

  assert {
    condition     = data.viettelcloud_subnet.by_cidr.id == viettelcloud_subnet.subnet.id
    error_message = "Subnet lookup by CIDR did not return the expected subnet."
  }

  assert {
    condition     = data.viettelcloud_subnet.by_vpc_id.id == viettelcloud_subnet.subnet.id
    error_message = "Subnet lookup by VPC ID did not return the expected subnet."
  }

  assert {
    condition     = data.viettelcloud_subnet.by_vpc_name.id == viettelcloud_subnet.subnet.id
    error_message = "Subnet lookup by VPC name did not return the expected subnet."
  }

  assert {
    condition = (
      data.viettelcloud_subnet.by_name_and_vpc.id == viettelcloud_subnet.subnet.id &&
      data.viettelcloud_subnet.by_name_and_vpc.name == "tf-test-subnet-ds-primary" &&
      data.viettelcloud_subnet.by_name_and_vpc.vpc_name == "tf-test-subnet-ds-vpc"
    )
    error_message = "Subnet lookup by name and VPC did not apply AND semantics."
  }

  assert {
    condition = (
      data.viettelcloud_subnet.by_cidr_and_region.id == viettelcloud_subnet.subnet.id &&
      data.viettelcloud_subnet.by_cidr_and_region.cidr == "10.90.1.0/24" &&
      data.viettelcloud_subnet.by_cidr_and_region.region == "vn-central-1"
    )
    error_message = "Subnet lookup by CIDR and region did not return the expected subnet."
  }

  # All criteria use the list path and preserve semantically equivalent input representations.
  assert {
    condition = (
      data.viettelcloud_subnet.by_all_filters.id == upper(viettelcloud_subnet.subnet.id) &&
      data.viettelcloud_subnet.by_all_filters.name == "  tf-test-subnet-ds-primary  " &&
      data.viettelcloud_subnet.by_all_filters.cidr == " 10.90.1.0/24 " &&
      data.viettelcloud_subnet.by_all_filters.vpc_id == upper(viettelcloud_vpc.vpc.id) &&
      data.viettelcloud_subnet.by_all_filters.vpc_name == " tf-test-subnet-ds-vpc " &&
      data.viettelcloud_subnet.by_all_filters.region == " vn-central-1 "
    )
    error_message = "Combined subnet lookup did not preserve equivalent configured filter values."
  }
}
