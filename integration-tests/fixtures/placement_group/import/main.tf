# Plan-only fixture for the import chain of
# tests/placement_group_resource.tftest.hcl. It imports the Placement Group
# that run "createPlacementGroup_importChainFixture_placementGroupCreated"
# created, rather than creating one of its own.
#
# Never apply it. That Placement Group already belongs to state_key "import",
# so a second owner would delete it twice at teardown.

terraform {
  required_version = ">= 1.12"

  required_providers {
    viettelcloud = {
      source = "viettelcloud/viettelcloud"
    }
  }
}

variable "placement_group_id" {
  type = string
}

variable "name" {
  type = string
}

variable "policy" {
  type = string
}

variable "region" {
  type = string
}

# Terraform accepts an import block only in a root module, and a run's module
# block replaces that run's root module. Writing the import inside the run
# block itself fails as an unsupported block type.
import {
  to = viettelcloud_placement_group.imported
  id = var.placement_group_id
}

# Only the required attributes are set, fed from the state of the run that
# created this Placement Group. description is optional and computed, so
# leaving it unset keeps whatever the import refresh read. Every remaining
# attribute is computed.
resource "viettelcloud_placement_group" "imported" {
  name   = var.name
  policy = var.policy
  region = var.region
}

output "placement_group_id" {
  value = viettelcloud_placement_group.imported.id
}
