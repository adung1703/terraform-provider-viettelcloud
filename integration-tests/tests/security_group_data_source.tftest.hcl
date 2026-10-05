# Test index — copy a run declaration below and Find Next to jump to its block.
# Chains and runs are listed in file order; setup runs are included.
#
# Chain lookup (shared fixture state):
#   run "querySupportedFilters_isolatedSecurityGroups_matchingObjectsAndConfiguredValuesReturned" {

provider "viettelcloud" {
  endpoint   = var.api_endpoint
  token      = var.token
  project_id = var.project_id
}

# Create one Security Group and verify every supported lookup filter.
run "querySupportedFilters_isolatedSecurityGroups_matchingObjectsAndConfiguredValuesReturned" {
  command = apply

  module {
    source = "./fixtures/security_group/datasource"
  }

  variables {
    region          = "vn-central-1"
    sg1_name        = "tf-test-ds-sg-1"
    sg1_description = "Security group 1 for data source test"
  }

  # Assert lookup by ID
  assert {
    condition = (
      data.viettelcloud_security_group.by_id.id == viettelcloud_security_group.sg1.id &&
      data.viettelcloud_security_group.by_id.name == "tf-test-ds-sg-1" &&
      data.viettelcloud_security_group.by_id.description == "Security group 1 for data source test" &&
      data.viettelcloud_security_group.by_id.region == "vn-central-1" &&
      length(data.viettelcloud_security_group.by_id.display_name) > 0 &&
      length(data.viettelcloud_security_group.by_id.created_at) > 0 &&
      length(data.viettelcloud_security_group.by_id.updated_at) > 0
    )
    error_message = "Security group lookup by ID returned unexpected values."
  }

  # Assert lookup by Name
  assert {
    condition = (
      data.viettelcloud_security_group.by_name.id == viettelcloud_security_group.sg1.id &&
      data.viettelcloud_security_group.by_name.name == "tf-test-ds-sg-1" &&
      data.viettelcloud_security_group.by_name.region == "vn-central-1"
    )
    error_message = "Security group lookup by Name returned unexpected values."
  }

  # Assert lookup by Name and Region
  assert {
    condition = (
      data.viettelcloud_security_group.by_name_and_region.id == viettelcloud_security_group.sg1.id &&
      data.viettelcloud_security_group.by_name_and_region.name == "tf-test-ds-sg-1" &&
      data.viettelcloud_security_group.by_name_and_region.region == "vn-central-1"
    )
    error_message = "Security group lookup by Name and Region returned unexpected values."
  }

  # Assert that multiple filters are combined with AND semantics.
  assert {
    condition = (
      data.viettelcloud_security_group.by_all_filters.id == viettelcloud_security_group.sg1.id &&
      data.viettelcloud_security_group.by_all_filters.name == "tf-test-ds-sg-1" &&
      data.viettelcloud_security_group.by_all_filters.region == "vn-central-1"
    )
    error_message = "Security group lookup by combined filters returned unexpected values."
  }

  # Assert lookup by is_default = true
  assert {
    condition = (
      data.viettelcloud_security_group.by_is_default.is_default == true &&
      length(data.viettelcloud_security_group.by_is_default.id) > 0
    )
    error_message = "Security group lookup by is_default returned unexpected values."
  }

  # Assert lookup by is_default = true and Region
  assert {
    condition = (
      data.viettelcloud_security_group.by_is_default_and_region.is_default == true &&
      data.viettelcloud_security_group.by_is_default_and_region.region == "vn-central-1" &&
      length(data.viettelcloud_security_group.by_is_default_and_region.id) > 0
    )
    error_message = "Security group lookup by is_default and Region returned unexpected values."
  }
}
