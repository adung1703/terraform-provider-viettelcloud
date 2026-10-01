terraform {
  required_version = ">= 1.12"

  required_providers {
    viettelcloud = {
      source = "viettelcloud/viettelcloud"
    }
  }
}

variable "region" {
  type = string
}

variable "zone" {
  type = string
}

# Names every resource this module creates. Each parallel chain in the test file
# passes its own prefix so the chains never share a name.
variable "name_prefix" {
  type = string
}

# The VPC every chain shares. The test file creates it once, because the backend
# allocates a route table per VPC and rejects creating several at once.
variable "vpc_id" {
  type = string
}

# First two octets of the shared VPC's address space, for example "10.36".
variable "vpc_cidr_prefix" {
  type    = string
  default = "10.36"
}

# Third octet of the /24 this module carves out of the shared VPC. Each chain
# passes its own index, and the second subnet takes the next octet up, so no two
# chains overlap.
variable "subnet_index" {
  type = number
}

# Which subnets the server allocates a private IP from: first, second, or both.
# The second subnet can outlive its attachment so detach and subnet deletion
# happen in separate applies.
variable "private_ip_mode" {
  type    = string
  default = "first"
}

# Reverses the configured attachment order without changing the attachment set.
variable "reverse_private_ips" {
  type    = bool
  default = false
}

variable "keep_second_subnet" {
  type    = bool
  default = false
}

# Creates a private IP managed independently of the server.
variable "create_managed_private_ip" {
  type    = bool
  default = false
}

# Attaches the independently managed private IP to the server by ID.
variable "attach_managed_private_ip" {
  type    = bool
  default = false
}

# Left unset unless a run exercises the name itself.
variable "server_name" {
  type    = string
  default = null
}

variable "server_description" {
  type    = string
  default = null
}

variable "image" {
  type = string
}

variable "volume_type" {
  type = string
}

# Left unset so the backend allocates the boot volume IOPS.
variable "boot_iops" {
  type    = number
  default = null
}

variable "boot_volume_size" {
  type    = number
  default = 30
}

variable "bandwidth" {
  type    = number
  default = null
}

variable "flavor_kind" {
  type    = string
  default = "predefined"
}

variable "predefined_flavor_name" {
  type    = string
  default = "b1.micro2x"
}

# The backend refuses a resize whose flavor resolves to the one the server
# already runs on, so these defaults must not describe the same capacity as
# `predefined_flavor_name`. `b1.micro2x` is 2 vCPU / 2 GB.
variable "flavor_vcpus" {
  type    = number
  default = 2
}

variable "flavor_ram" {
  type    = number
  default = 4
}

variable "data_volume_size" {
  type    = number
  default = 20
}

# The second data volume is opt-in because most chains only need one. It proves
# data_volumes[i].id binds each element to its own backend volume, which a
# single-volume list cannot show.
variable "second_data_volume_size" {
  type    = number
  default = null
}

# Left unset so a run that omits them exercises the provider default: the boot
# volume and the data volume are deleted with the server.
variable "boot_delete_on_termination" {
  type    = bool
  default = null
}

variable "data_volume_delete_on_termination" {
  type    = bool
  default = null
}

variable "elastic_ip_count" {
  type    = number
  default = 0
}

variable "power_state" {
  type    = string
  default = null
}

variable "user_data" {
  type    = string
  default = null
}

variable "key_pair_id" {
  type    = string
  default = null
}

variable "placement_group_id" {
  type    = string
  default = null
}

variable "security_group_ids" {
  type    = set(string)
  default = null
}

resource "viettelcloud_subnet" "subnet" {
  name   = "${var.name_prefix}-subnet"
  cidr   = "${var.vpc_cidr_prefix}.${var.subnet_index}.0/24"
  vpc_id = var.vpc_id
}

resource "viettelcloud_subnet" "second" {
  count = local.needs_second_subnet ? 1 : 0

  name   = "${var.name_prefix}-subnet-second"
  cidr   = "${var.vpc_cidr_prefix}.${var.subnet_index + 1}.0/24"
  vpc_id = var.vpc_id
}

# A private IP the practitioner owns. The server may attach it by ID, and
# detaching it must never delete it.
resource "viettelcloud_private_ip" "managed" {
  count = var.create_managed_private_ip ? 1 : 0

  subnet_id   = viettelcloud_subnet.subnet.id
  description = "Private IP managed independently and attached to the server by ID"
}

locals {
  needs_second_subnet = var.keep_second_subnet || contains(["second", "both"], var.private_ip_mode)

  subnet_ids = concat(
    contains(["first", "both"], var.private_ip_mode) ? [viettelcloud_subnet.subnet.id] : [],
    contains(["second", "both"], var.private_ip_mode) ? viettelcloud_subnet.second[*].id : [],
  )

  subnet_private_ips = [for subnet_id in local.subnet_ids : {
    kind        = "subnet"
    id          = null
    subnet_id   = subnet_id
    subnet_cidr = null
  }]

  managed_private_ips = var.attach_managed_private_ip ? [{
    kind                  = "ip"
    id                    = viettelcloud_private_ip.managed[0].id
    subnet_id             = null
    subnet_cidr           = null
    delete_on_termination = false
  }] : []

  flavor = var.flavor_kind == "predefined" ? {
    kind   = "predefined"
    name   = var.predefined_flavor_name
    family = null
    vcpus  = null
    ram    = null
    } : {
    kind   = "custom"
    name   = null
    family = "basic"
    vcpus  = var.flavor_vcpus
    ram    = var.flavor_ram
  }
}

resource "viettelcloud_server" "server" {
  name               = var.server_name == null ? "${var.name_prefix}-server" : var.server_name
  description        = var.server_description
  zone               = var.zone
  bandwidth          = var.bandwidth
  power_state        = var.power_state
  user_data          = var.user_data
  key_pair_id        = var.key_pair_id
  placement_group_id = var.placement_group_id
  security_group_ids = var.security_group_ids

  flavor = local.flavor

  boot = {
    boot_type             = "image"
    image                 = var.image
    volume_type           = var.volume_type
    volume_size           = var.boot_volume_size
    iops                  = var.boot_iops
    delete_on_termination = var.boot_delete_on_termination
  }

  data_volumes = concat(
    [{
      volume_type           = var.volume_type
      volume_size           = var.data_volume_size
      delete_on_termination = var.data_volume_delete_on_termination
    }],
    var.second_data_volume_size == null ? [] : [{
      volume_type           = var.volume_type
      volume_size           = var.second_data_volume_size
      delete_on_termination = var.data_volume_delete_on_termination
    }],
  )

  private_ips = concat(
    var.reverse_private_ips ? reverse(local.subnet_private_ips) : local.subnet_private_ips,
    local.managed_private_ips,
  )

  elastic_ips = [for index in range(var.elastic_ip_count) : {
    kind        = "new"
    enable_ipv4 = true
  }]
}

output "server_id" {
  value = viettelcloud_server.server.id
}

output "key_pair_id" {
  value = viettelcloud_server.server.key_pair_id
}

output "key_pair_name" {
  value = viettelcloud_server.server.key_pair_name
}

output "placement_group_id" {
  value = viettelcloud_server.server.placement_group_id
}

output "placement_group_name" {
  value = viettelcloud_server.server.placement_group_name
}

output "security_group_ids" {
  value = viettelcloud_server.server.security_group_ids
}

output "private_ip_id" {
  value = viettelcloud_server.server.private_ips[0].id
}

output "private_ip_ids" {
  value = viettelcloud_server.server.private_ips[*].id
}

output "elastic_ip_ids" {
  value = viettelcloud_server.server.elastic_ips[*].id
}

output "managed_private_ip_id" {
  value = one(viettelcloud_private_ip.managed[*].id)
}

output "data_volume_ids" {
  value = viettelcloud_server.server.data_volume_ids
}

# Per-element IDs follow data_volumes order, while data_volume_ids follows the
# order the server reported. A later run compares against this one to show the
# element kept the volume it was created with.
output "data_volume_element_ids" {
  value = viettelcloud_server.server.data_volumes[*].id
}
