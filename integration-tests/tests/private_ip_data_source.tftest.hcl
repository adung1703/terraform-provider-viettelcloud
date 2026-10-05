# Test index — copy a run declaration below and Find Next to jump to its block.
# Chains and runs are listed in file order; setup runs are included.
#
# Chain lookup (shared fixture state):
#   run "querySupportedFilters_isolatedPrivateIps_matchingObjectsAndConfiguredValuesReturned" {

provider "viettelcloud" {
  endpoint   = var.api_endpoint
  token      = var.token
  project_id = var.project_id
}

# Selector coverage matrix:
#
# | #  | Representative selector pattern                         | Fixture                         | Covered |
# |----|---------------------------------------------------------|---------------------------------|---------|
# | 1  | id                                                      | by_id                           | yes     |
# | 2  | ip_address                                              | —                               | no      |
# | 3  | ip_address + subnet_id                                  | by_subnet_id                    | yes     |
# | 4  | ip_address + vpc_id + subnet_id                         | by_parent_ids                   | yes     |
# | 5  | ip_address + vpc_name + subnet_name                     | by_names                        | yes     |
# | 6  | ip_address + vpc_id + subnet_name                       | by_vpc_id_and_subnet_name       | yes     |
# | 7  | ip_address + subnet_name + region                       | by_subnet_name_and_region       | yes     |
# | 8  | ip_address + subnet_cidr + region                       | by_subnet_cidr_and_region       | yes     |
# | 9  | ip_address + vpc_name + region                          | by_vpc_name_and_region          | yes     |
# | 10 | ip_address + vpc_name + subnet_cidr                     | by_vpc_name_and_subnet_cidr     | yes     |
# | 11 | ip_address + vpc_cidr + subnet_name                     | by_vpc_cidr_and_subnet_name     | yes     |
# | 12 | ip_address + full VPC/subnet name/CIDR/region hierarchy | by_hierarchy                    | yes     |
# | 13 | ip_address + vpc_cidr + subnet_cidr                     | by_vpc_and_subnet_cidrs         | yes     |
# | 14 | id + ip_address + every parent selector                 | by_all_filters                  | yes     |
#
# Covered: 13/14 representative happy-path patterns = 92.9%.
# ip_address-only is excluded because duplicate addresses can exist across
# subnets or VPCs, making that live lookup inherently ambiguous and flaky.
# The fixture creates and later destroys the VPC, subnet, target Private IP,
# and allowed VIP used by every lookup below.
run "querySupportedFilters_isolatedPrivateIps_matchingObjectsAndConfiguredValuesReturned" {
  command = apply

  module {
    source = "./fixtures/private_ip/datasource"
  }

  variables {
    region             = "vn-central-1"
    vpc_name           = "tf-test-private-ip-ds-vpc"
    vpc_cidr           = "10.81.0.0/16"
    subnet_name        = "tf-test-private-ip-ds-subnet"
    subnet_cidr        = "10.81.1.0/24"
    private_ip_address = "10.81.1.100"
  }

  assert {
    condition = (
      data.viettelcloud_private_ip.by_id.id == viettelcloud_private_ip.target.id &&
      data.viettelcloud_private_ip.by_id.description == "Private IP for data source integration tests" &&
      data.viettelcloud_private_ip.by_id.ip_address == "10.81.1.100" &&
      length(data.viettelcloud_private_ip.by_id.mac_address) > 0 &&
      data.viettelcloud_private_ip.by_id.subnet_id == viettelcloud_subnet.test.id &&
      data.viettelcloud_private_ip.by_id.subnet_name == "tf-test-private-ip-ds-subnet" &&
      data.viettelcloud_private_ip.by_id.subnet_cidr == "10.81.1.0/24" &&
      data.viettelcloud_private_ip.by_id.vpc_id == viettelcloud_vpc.test.id &&
      data.viettelcloud_private_ip.by_id.vpc_name == "tf-test-private-ip-ds-vpc" &&
      data.viettelcloud_private_ip.by_id.vpc_cidr == null &&
      data.viettelcloud_private_ip.by_id.region == "vn-central-1" &&
      length(data.viettelcloud_private_ip.by_id.device_owner) > 0 &&
      length(data.viettelcloud_private_ip.by_id.display_name) > 0 &&
      data.viettelcloud_private_ip.by_id.port_security != null &&
      ((data.viettelcloud_private_ip.by_id.server_id == null) == (data.viettelcloud_private_ip.by_id.server_name == null)) &&
      try(toset(data.viettelcloud_private_ip.by_id.allowed_cidrs) == data.viettelcloud_private_ip.by_id.allowed_cidrs, data.viettelcloud_private_ip.by_id.allowed_cidrs == null) &&
      data.viettelcloud_private_ip.by_id.allowed_vip_ids == toset([viettelcloud_private_ip.vip.id]) &&
      try(length(data.viettelcloud_private_ip.by_id.attached_vips), 0) == 1 &&
      tolist(data.viettelcloud_private_ip.by_id.attached_vips)[0].id == viettelcloud_private_ip.vip.id &&
      tolist(data.viettelcloud_private_ip.by_id.attached_vips)[0].ip_address == viettelcloud_private_ip.vip.ip_address &&
      length(data.viettelcloud_private_ip.by_id.created_at) > 0 &&
      length(data.viettelcloud_private_ip.by_id.updated_at) > 0
    )
    error_message = "Private IP lookup by ID returned incomplete or unexpected state."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_subnet_id.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_subnet_id.subnet_id == data.viettelcloud_private_ip.by_id.subnet_id
    )
    error_message = "Private IP lookup by subnet ID did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_parent_ids.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_parent_ids.vpc_id == data.viettelcloud_private_ip.by_id.vpc_id &&
      data.viettelcloud_private_ip.by_parent_ids.subnet_id == data.viettelcloud_private_ip.by_id.subnet_id
    )
    error_message = "Private IP lookup by parent IDs did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_names.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_names.vpc_name == data.viettelcloud_private_ip.by_id.vpc_name &&
      data.viettelcloud_private_ip.by_names.subnet_name == data.viettelcloud_private_ip.by_id.subnet_name
    )
    error_message = "Private IP lookup by parent names did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_vpc_id_and_subnet_name.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_vpc_id_and_subnet_name.vpc_id == data.viettelcloud_private_ip.by_id.vpc_id &&
      data.viettelcloud_private_ip.by_vpc_id_and_subnet_name.subnet_name == data.viettelcloud_private_ip.by_id.subnet_name
    )
    error_message = "Private IP lookup by VPC ID and subnet name did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_subnet_name_and_region.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_subnet_name_and_region.subnet_name == data.viettelcloud_private_ip.by_id.subnet_name &&
      data.viettelcloud_private_ip.by_subnet_name_and_region.region == data.viettelcloud_private_ip.by_id.region
    )
    error_message = "Private IP lookup by subnet name and region did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_subnet_cidr_and_region.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_subnet_cidr_and_region.subnet_cidr == data.viettelcloud_private_ip.by_id.subnet_cidr &&
      data.viettelcloud_private_ip.by_subnet_cidr_and_region.region == data.viettelcloud_private_ip.by_id.region
    )
    error_message = "Private IP lookup by subnet CIDR and region did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_vpc_name_and_region.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_vpc_name_and_region.vpc_name == data.viettelcloud_private_ip.by_id.vpc_name &&
      data.viettelcloud_private_ip.by_vpc_name_and_region.region == data.viettelcloud_private_ip.by_id.region
    )
    error_message = "Private IP lookup by VPC name and region did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_vpc_name_and_subnet_cidr.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_vpc_name_and_subnet_cidr.vpc_name == data.viettelcloud_private_ip.by_id.vpc_name &&
      data.viettelcloud_private_ip.by_vpc_name_and_subnet_cidr.subnet_cidr == data.viettelcloud_private_ip.by_id.subnet_cidr
    )
    error_message = "Private IP lookup by VPC name and subnet CIDR did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_vpc_cidr_and_subnet_name.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_vpc_cidr_and_subnet_name.vpc_cidr == data.viettelcloud_vpc.parent.cidr &&
      data.viettelcloud_private_ip.by_vpc_cidr_and_subnet_name.subnet_name == data.viettelcloud_private_ip.by_id.subnet_name
    )
    error_message = "Private IP lookup by VPC CIDR and subnet name did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_hierarchy.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_hierarchy.vpc_cidr == data.viettelcloud_vpc.parent.cidr &&
      data.viettelcloud_private_ip.by_hierarchy.subnet_cidr == data.viettelcloud_private_ip.by_id.subnet_cidr &&
      data.viettelcloud_private_ip.by_hierarchy.region == data.viettelcloud_private_ip.by_id.region
    )
    error_message = "Private IP hierarchy lookup did not return the expected object."
  }

  assert {
    condition = (
      lower(data.viettelcloud_private_ip.by_vpc_and_subnet_cidrs.id) == lower(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_vpc_and_subnet_cidrs.vpc_cidr == data.viettelcloud_vpc.parent.cidr &&
      data.viettelcloud_private_ip.by_vpc_and_subnet_cidrs.subnet_cidr == data.viettelcloud_private_ip.by_id.subnet_cidr
    )
    error_message = "Private IP lookup by VPC and subnet CIDRs did not return the expected object."
  }

  assert {
    condition = (
      data.viettelcloud_private_ip.by_all_filters.id == upper(data.viettelcloud_private_ip.by_id.id) &&
      data.viettelcloud_private_ip.by_all_filters.ip_address == " ${data.viettelcloud_private_ip.by_id.ip_address} " &&
      data.viettelcloud_private_ip.by_all_filters.subnet_id == upper(data.viettelcloud_private_ip.by_id.subnet_id) &&
      data.viettelcloud_private_ip.by_all_filters.subnet_name == " ${data.viettelcloud_private_ip.by_id.subnet_name} " &&
      data.viettelcloud_private_ip.by_all_filters.subnet_cidr == " ${data.viettelcloud_private_ip.by_id.subnet_cidr} " &&
      data.viettelcloud_private_ip.by_all_filters.vpc_id == upper(data.viettelcloud_private_ip.by_id.vpc_id) &&
      data.viettelcloud_private_ip.by_all_filters.vpc_name == " ${data.viettelcloud_private_ip.by_id.vpc_name} " &&
      data.viettelcloud_private_ip.by_all_filters.vpc_cidr == " ${data.viettelcloud_vpc.parent.cidr} " &&
      data.viettelcloud_private_ip.by_all_filters.region == " ${data.viettelcloud_private_ip.by_id.region} "
    )
    error_message = "Combined Private IP lookup did not preserve equivalent configured filter values."
  }
}
