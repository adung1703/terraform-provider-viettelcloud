# Plan-only fixture for the import chain of
# tests/server_resource.tftest.hcl. It imports the server that run
# "createServer_importFixtureWithVolumeAndPrivateIp_serverWithVolumeAndPrivateIpReturned"
# created, so it declares no subnet, private IP, or volume of its own.
#
# Never apply it. That server already belongs to state_key "import", so a
# second owner would delete it twice at teardown.

terraform {
  required_version = ">= 1.12"

  required_providers {
    viettelcloud = {
      source = "viettelcloud/viettelcloud"
    }
  }
}

variable "server_id" {
  type = string
}

variable "server_name" {
  type = string
}

variable "region" {
  type = string
}

variable "zone" {
  type = string
}

variable "image" {
  type = string
}

variable "volume_type" {
  type = string
}

variable "boot_volume_size" {
  type    = number
  default = 30
}

variable "data_volume_size" {
  type    = number
  default = 20
}

variable "flavor_kind" {
  type    = string
  default = "predefined"
}

variable "predefined_flavor_name" {
  type = string
}

# Declared because the test file sets them for every run. The imported server
# already carries these values and every one of them is computed, so restating
# them here would only risk a diff the backend, not the configuration, owns.
variable "bandwidth" {
  type    = number
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

# Terraform accepts an import block only in a root module, and a run's module
# block replaces that run's root module. Writing the import inside the run
# block itself fails as an unsupported block type.
import {
  to = viettelcloud_server.imported
  id = var.server_id
}

# Only the attributes whose omission would plan a change are configured. name,
# flavor, and boot are required. data_volumes is optional but forces
# replacement, so a null list would not match the imported one. Everything else
# is computed and keeps whatever the refresh read.
resource "viettelcloud_server" "imported" {
  name = var.server_name
  zone = var.zone

  flavor = {
    kind   = var.flavor_kind
    name   = var.predefined_flavor_name
    family = null
    vcpus  = null
    ram    = null
  }

  boot = {
    boot_type   = "image"
    image       = var.image
    volume_type = var.volume_type
    volume_size = var.boot_volume_size
  }

  # iops and delete_on_termination are computed, so the import records whatever
  # the backend allocated and the provider defaulted. Leaving them unset here
  # keeps those values instead of planning a change.
  data_volumes = [{
    volume_type = var.volume_type
    volume_size = var.data_volume_size
  }]
}

output "server_id" {
  value = viettelcloud_server.imported.id
}
