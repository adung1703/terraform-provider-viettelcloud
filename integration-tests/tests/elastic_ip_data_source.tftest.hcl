# Test index — copy a run declaration below and Find Next to jump to its block.
# Chains and runs are listed in file order; setup runs are included.
#
# Chain lookup (shared fixture state):
#   run "querySupportedFilters_isolatedElasticIps_matchingObjectsAndConfiguredValuesReturned" {

provider "viettelcloud" {
  endpoint   = var.api_endpoint
  token      = var.token
  project_id = var.project_id
}

# Create two Elastic IPs and verify every supported lookup criterion.
run "querySupportedFilters_isolatedElasticIps_matchingObjectsAndConfiguredValuesReturned" {
  command = apply

  module {
    source = "./fixtures/elastic_ip/datasource"
  }

  variables {
    region                 = "vn-central-1"
    dual_stack_description = "tf-test-ds-eip-dual-stack"
    ipv4_only_description  = "tf-test-ds-eip-ipv4-only"
  }

  # Assert lookup by ID returns every attribute of the dual-stack Elastic IP.
  assert {
    condition = (
      data.viettelcloud_elastic_ip.by_id.id == viettelcloud_elastic_ip.dual_stack.id &&
      data.viettelcloud_elastic_ip.by_id.description == "tf-test-ds-eip-dual-stack" &&
      data.viettelcloud_elastic_ip.by_id.region == "vn-central-1" &&
      data.viettelcloud_elastic_ip.by_id.enable_ipv4 &&
      data.viettelcloud_elastic_ip.by_id.enable_ipv6 &&
      data.viettelcloud_elastic_ip.by_id.ip_address == viettelcloud_elastic_ip.dual_stack.ip_address &&
      data.viettelcloud_elastic_ip.by_id.ipv6_address == viettelcloud_elastic_ip.dual_stack.ipv6_address &&
      length(data.viettelcloud_elastic_ip.by_id.status) > 0 &&
      length(data.viettelcloud_elastic_ip.by_id.created_at) > 0 &&
      length(data.viettelcloud_elastic_ip.by_id.updated_at) > 0 &&
      data.viettelcloud_elastic_ip.by_id.available == null
    )
    error_message = "Elastic IP lookup by ID returned unexpected values."
  }

  # Assert lookup by IPv4 address selects the IPv4-only Elastic IP.
  assert {
    condition = (
      data.viettelcloud_elastic_ip.by_ip_address.id == viettelcloud_elastic_ip.ipv4_only.id &&
      data.viettelcloud_elastic_ip.by_ip_address.ip_address == viettelcloud_elastic_ip.ipv4_only.ip_address &&
      data.viettelcloud_elastic_ip.by_ip_address.description == "tf-test-ds-eip-ipv4-only" &&
      !data.viettelcloud_elastic_ip.by_ip_address.enable_ipv6 &&
      data.viettelcloud_elastic_ip.by_ip_address.ipv6_address == null
    )
    error_message = "Elastic IP lookup by IPv4 address returned unexpected values."
  }

  # Assert lookup by IPv6 address selects the dual-stack Elastic IP.
  assert {
    condition = (
      data.viettelcloud_elastic_ip.by_ipv6_address.id == viettelcloud_elastic_ip.dual_stack.id &&
      data.viettelcloud_elastic_ip.by_ipv6_address.ipv6_address == viettelcloud_elastic_ip.dual_stack.ipv6_address &&
      data.viettelcloud_elastic_ip.by_ipv6_address.description == "tf-test-ds-eip-dual-stack"
    )
    error_message = "Elastic IP lookup by IPv6 address returned unexpected values."
  }

  # Assert lookup by status combined with the unique address.
  assert {
    condition = (
      data.viettelcloud_elastic_ip.by_status_and_ip_address.id == viettelcloud_elastic_ip.ipv4_only.id &&
      data.viettelcloud_elastic_ip.by_status_and_ip_address.status == viettelcloud_elastic_ip.ipv4_only.status
    )
    error_message = "Elastic IP lookup by status and IPv4 address returned unexpected values."
  }

  # Assert lookup by region combined with the unique address.
  assert {
    condition = (
      data.viettelcloud_elastic_ip.by_region_and_ip_address.id == viettelcloud_elastic_ip.dual_stack.id &&
      data.viettelcloud_elastic_ip.by_region_and_ip_address.region == "vn-central-1"
    )
    error_message = "Elastic IP lookup by region and IPv4 address returned unexpected values."
  }

  # Assert the availability filter matches an Elastic IP no server holds.
  assert {
    condition = (
      data.viettelcloud_elastic_ip.by_available_and_ip_address.id == viettelcloud_elastic_ip.ipv4_only.id &&
      data.viettelcloud_elastic_ip.by_available_and_ip_address.available
    )
    error_message = "Elastic IP lookup by availability and IPv4 address returned unexpected values."
  }

  # Assert every filter combined, and that configured values survive in state.
  assert {
    condition = (
      data.viettelcloud_elastic_ip.by_all_filters.id == upper(viettelcloud_elastic_ip.dual_stack.id) &&
      data.viettelcloud_elastic_ip.by_all_filters.ip_address == " ${viettelcloud_elastic_ip.dual_stack.ip_address} " &&
      data.viettelcloud_elastic_ip.by_all_filters.ipv6_address == " ${viettelcloud_elastic_ip.dual_stack.ipv6_address} " &&
      data.viettelcloud_elastic_ip.by_all_filters.region == " vn-central-1 " &&
      data.viettelcloud_elastic_ip.by_all_filters.description == "tf-test-ds-eip-dual-stack" &&
      data.viettelcloud_elastic_ip.by_all_filters.enable_ipv4 &&
      data.viettelcloud_elastic_ip.by_all_filters.enable_ipv6 &&
      length(data.viettelcloud_elastic_ip.by_all_filters.status) > 0
    )
    error_message = "Elastic IP lookup with all filters did not preserve the configured values."
  }
}
