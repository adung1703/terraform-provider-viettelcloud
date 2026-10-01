# Plan-only fixture for the import chain of
# tests/security_group_resource.tftest.hcl. It imports the security group that
# run "createSecurityGroup_importChainFixture_securityGroupCreated" created,
# rather than creating one of its own.
#
# Never apply it. That security group already belongs to state_key "import", so
# a second owner would delete it twice at teardown.

terraform {
  required_version = ">= 1.12"

  required_providers {
    viettelcloud = {
      source = "viettelcloud/viettelcloud"
    }
  }
}

variable "security_group_id" {
  type = string
}

variable "name" {
  type = string
}

variable "region" {
  type = string
}

# Terraform accepts an import block only in a root module, and a run's module
# block replaces that run's root module. Writing the import inside the run
# block itself fails as an unsupported block type.
import {
  to = viettelcloud_security_group.imported
  id = var.security_group_id
}

# Only the required attributes are set, fed from the state of the run that
# created this security group. description is optional and computed, so leaving
# it unset keeps whatever the import refresh read. Every remaining attribute is
# computed.
resource "viettelcloud_security_group" "imported" {
  name   = var.name
  region = var.region
}

output "security_group_id" {
  value = viettelcloud_security_group.imported.id
}
