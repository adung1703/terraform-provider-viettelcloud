# Test index — copy a run declaration below and Find Next to jump to its block.
# Chains and runs are listed in file order; setup runs are included.
#
# Chain network (shared setup; state_key = "network"):
#   run "createVpc_isolatedNetwork_validVpcIdReturned" {
#
# Chain base (state_key = "base"):
#   run "createServer_optionalServerValuesOmitted_runtimeVolumesAndPrivateIpReturned" {
#   run "planAgain_createdServer_stateStable" {
#
# Chain mutable_fields (state_key = "mutable_fields"):
#   run "createServer_nameSetDescriptionOmittedAndBandwidth300_serverCreated" {
#   run "changeNameDescriptionAndBandwidth_existingServer_updatedInPlace" {
#   run "omitDescription_serverDescriptionSet_descriptionPreservedInPlace" {
#   run "clearDescriptionAndSetUntrimmedName_serverDescriptionSet_emptyDescriptionAndNamePreserved" {
#   run "reapply_serverWithClearedDescriptionAndUntrimmedName_identityAndStatePreserved" {
#
# Chain bandwidth (state_key = "bandwidth"):
#   run "createServer_bandwidth400_serverCreated" {
#   run "changeBandwidth_serverWithBandwidth400_updatedInPlace" {
#
# Chain private_ip (state_key = "private_ip"):
#   run "createServer_singleSubnetAttachment_privateIpAllocated" {
#   run "attachSecondSubnet_oneSubnetAttachment_twoPrivateIpsWithFirstPreserved" {
#   run "reverseOrder_twoSubnetAttachments_privateIpIdsReordered" {
#   run "removeFirstSubnet_twoSubnetAttachments_secondPrivateIpPreserved" {
#   run "returnToFirstSubnet_secondSubnetAttachment_privateIpReallocated" {
#   run "deleteSubnet_detachedSecondSubnet_subnetRemovedAndServerPreserved" {
#
# Chain existing_ip (state_key = "existing_ip"):
#   run "createServer_singleSubnetIpBeforeManagedIpAttach_serverCreated" {
#   run "attachManagedPrivateIp_serverWithSubnetIp_twoAttachmentsWithManagedIpPreserved" {
#   run "detachManagedPrivateIp_managedIpAttachment_managedIpPreserved" {
#
# Chain elastic_ip (state_key = "elastic_ip"):
#   run "createServer_noElasticIpConfig_serverRunningWithoutElasticIps" {
#   run "attachElasticIp_serverWithoutElasticIps_allocatedAddressReturned" {
#   run "attachSecondElasticIp_oneElasticIpAttachment_firstAttachmentPreserved" {
#   run "removeSecondElasticIp_twoElasticIpAttachments_firstAttachmentPreserved" {
#   run "setEmptyElasticIpList_twoElasticIpAttachments_elasticIpsDetachedAndPrivateIpPreserved" {
#
# Chain power (state_key = "power"):
#   run "createServer_powerStateOmitted_serverCreated" {
#   run "requestShutdown_runningServer_serverStoppedInPlace" {
#   run "planAgain_stoppedServer_powerStateStable" {
#   run "requestRunning_stoppedServer_serverStartedInPlace" {
#
# Chain rebuild (state_key = "rebuild"):
#   run "createServer_predefinedFlavorAndBootImage_configuredFlavorAndImageReturned" {
#   run "changeImage_existingBootImage_serverRebuiltInPlace" {
#   run "restoreOriginalImage_rebuiltServer_serverRebuiltInPlace" {
#   run "rebuildAndResizeRunningServer_newFlavorAndImage_rebuiltAndResizedInPlace" {
#
# Chain user_data (state_key = "user_data"):
#   run "createServer_userDataOmitted_serverCreated" {
#   run "configureUserData_serverWithoutUserData_serverAndPrivateIpReplaced" {
#   run "planAgain_configuredUserData_serverIdentityPreserved" {
#
# Chain data_volume (state_key = "data_volume"):
#   run "createServer_dataVolumeSize20_serverCreated" {
#   run "increaseBootVolumeSize_existingServer_onlyBootVolumeResizedInPlace" {
#   run "increaseDataVolumeSize_existingServer_onlySecondDataVolumeResizedInPlace" {
#   run "increaseBootAndDataVolumeSizes_existingServer_allVolumesResizedInPlace" {
#   run "clearDeleteOnTermination_volumesDeletedOnTermination_volumesKeptInPlace" {
#   run "omitDeleteOnTermination_volumesKeptOnTermination_keptValuePreserved" {
#   run "restoreDeleteOnTermination_volumesKeptOnTermination_volumesDeletedInPlace" {
#
# Chain to_custom (state_key = "to_custom"):
#   run "createServer_predefinedFlavorForCustomSwitch_serverCreated" {
#   run "switchToCustom_predefinedFlavor_serverResizedInPlace" {
#
# Chain custom_resize (state_key = "custom_resize"):
#   run "createServer_predefinedFlavorForCapacityResize_serverCreated" {
#   run "switchToCustom_predefinedFlavorForCapacityResize_serverResizedInPlace" {
#   run "changeCapacity_customFlavor_serverResizedInPlace" {
#
# Chain to_predefined (state_key = "to_predefined"):
#   run "createServer_predefinedFlavorForRoundTripResize_serverCreated" {
#   run "switchToCustom_predefinedFlavorForRoundTripResize_serverResizedInPlace" {
#   run "changeCapacity_customFlavorBeforePredefinedSwitch_serverResizedInPlace" {
#   run "switchToPredefined_resizedCustomFlavor_serverResizedInPlace" {
#
# Chain import (state_key = "import" -> "import_target"; plan-only import):
#   run "createServer_importFixtureWithVolumeAndPrivateIp_serverWithVolumeAndPrivateIpReturned" {
#   run "planImport_existingServerId_runtimeAndAttachmentStateRestored" {
#
# Chain offline_operations (state_key = "offline_operations"):
#   run "createServer_stoppedServer_serverCreatedShutdown" {
#   run "resizeStoppedServer_customFlavor_serverResizedInPlaceWhileShutdown" {
#   run "rebuildStoppedServer_newImage_serverRebuiltInPlaceWhileShutdown" {
#   run "rebuildAndStartServer_stoppedAfterOfflineOperations_serverRebuiltAndStarted" {

# Server resource integration test suite: 15 independent server chains,
# 16 server creates including one destroy-before-create replacement;
# never more than 15 live servers at once. All lifecycle cases and assertions
# are retained. Only the shared VPC crosses chain boundaries. Import remains
# plan-only with a single real owner.

variable "token" {
  type      = string
  sensitive = true
}

variable "project_id" {
  type = string
}

variable "api_endpoint" {
  type = string
}

variable "keypair_id" {
  type    = string
  default = null
}

variable "placementgroup_id" {
  type    = string
  default = null
}

variable "security_group_id" {
  type    = string
  default = null
}

provider "viettelcloud" {
  endpoint   = var.api_endpoint
  token      = var.token
  project_id = var.project_id
}

variables {
  region      = "vn-central-1"
  zone        = "vn-central-1a"
  image       = "cirros"
  volume_type = "Ceph_HDD_1A_10K"
  bandwidth   = 300

  flavor_kind            = "predefined"
  predefined_flavor_name = "b1.micro2x"

  key_pair_id        = var.keypair_id
  placement_group_id = var.placementgroup_id
  security_group_ids = var.security_group_id == null ? null : [var.security_group_id]
}


run "createVpc_isolatedNetwork_validVpcIdReturned" {
  command   = apply
  state_key = "network"

  module {
    source = "./fixtures/server/network"
  }

  variables {
    name   = "tf-test-server-vpc"
    cidr   = "10.36.0.0/16"
    region = var.region
  }

  assert {
    condition     = length(viettelcloud_vpc.shared.id) > 0 && can(regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", viettelcloud_vpc.shared.id))
    error_message = "The VPC creation did not return a valid UUID."
  }
}

# Chain base.

run "createServer_optionalServerValuesOmitted_runtimeVolumesAndPrivateIpReturned" {
  command   = apply
  parallel  = true
  state_key = "base"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix        = "tf-test-server-base"
    subnet_index       = 101
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name        = "tf-test-server-base-before"
    server_description = null
  }

  assert {
    condition = (
      length(viettelcloud_server.server.id) > 0 &&
      viettelcloud_server.server.name == "tf-test-server-base-before" &&
      viettelcloud_server.server.description == "" &&
      viettelcloud_server.server.zone == "vn-central-1a" &&
      viettelcloud_server.server.quantity == 1 &&
      viettelcloud_server.server.bandwidth == 300 &&
      viettelcloud_server.server.power_state == "running" &&
      viettelcloud_server.server.flavor.kind == "predefined" &&
      viettelcloud_server.server.flavor.name == "b1.micro2x" &&
      viettelcloud_server.server.flavor.vcpus == null &&
      viettelcloud_server.server.flavor.ram == null &&
      viettelcloud_server.server.key_pair_id == var.key_pair_id &&
      viettelcloud_server.server.placement_group_id == var.placement_group_id &&
      (var.security_group_ids == null ? true : length(setsubtract(var.security_group_ids, viettelcloud_server.server.security_group_ids)) == 0) &&
      length(viettelcloud_server.server.status) > 0 &&
      length(viettelcloud_server.server.server_type) > 0 &&
      length(viettelcloud_server.server.created_at) > 0 &&
      length(viettelcloud_server.server.private_ips) == 1 &&
      length(viettelcloud_server.server.private_ips[0].id) > 0 &&
      length(viettelcloud_server.server.private_ips[0].ip_address) > 0 &&
      viettelcloud_server.server.private_ips[0].delete_on_termination &&
      length(viettelcloud_server.server.elastic_ips) == 0 &&
      length(viettelcloud_server.server.data_volume_ids) == 1 &&
      viettelcloud_server.server.boot.volume_size == 30 &&
      viettelcloud_server.server.boot.delete_on_termination &&
      viettelcloud_server.server.data_volumes[0].delete_on_termination
    )
    error_message = "Server was not created with the expected runtime, volume, and private IP state."
  }
}

run "planAgain_createdServer_stateStable" {
  command   = plan
  parallel  = true
  state_key = "base"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix        = "tf-test-server-base"
    subnet_index       = 101
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name        = "tf-test-server-base-before"
    server_description = null
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_optionalServerValuesOmitted_runtimeVolumesAndPrivateIpReturned.server_id &&
      viettelcloud_server.server.key_pair_id == var.key_pair_id &&
      viettelcloud_server.server.placement_group_id == var.placement_group_id &&
      (var.security_group_ids == null ? true : length(setsubtract(var.security_group_ids, viettelcloud_server.server.security_group_ids)) == 0) &&
      viettelcloud_server.server.private_ips[0].id == run.createServer_optionalServerValuesOmitted_runtimeVolumesAndPrivateIpReturned.private_ip_id &&
      viettelcloud_server.server.data_volume_ids == run.createServer_optionalServerValuesOmitted_runtimeVolumesAndPrivateIpReturned.data_volume_ids &&
      viettelcloud_server.server.boot.image == "cirros" &&
      viettelcloud_server.server.boot.volume_type == "Ceph_HDD_1A_10K" &&
      viettelcloud_server.server.boot.volume_size == 30 &&
      viettelcloud_server.server.flavor.kind == "predefined" &&
      viettelcloud_server.server.flavor.name == "b1.micro2x"
    )
    error_message = "Replanning the created configuration reported drift or planned a replacement."
  }
}


# Chain mutable_fields.

run "createServer_nameSetDescriptionOmittedAndBandwidth300_serverCreated" {
  command   = apply
  parallel  = true
  state_key = "mutable_fields"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix        = "tf-test-server-mutable-fields"
    subnet_index       = 111
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name        = "tf-test-server-mutable-fields-before"
    server_description = null
  }

  assert {
    condition     = length(viettelcloud_server.server.id) > 0 && length(viettelcloud_server.server.private_ips) == 1 && length(viettelcloud_server.server.data_volume_ids) == 1
    error_message = "The mutable-fields chain's prerequisite server was not created."
  }
}

run "changeNameDescriptionAndBandwidth_existingServer_updatedInPlace" {
  command   = apply
  parallel  = true
  state_key = "mutable_fields"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix        = "tf-test-server-mutable-fields"
    subnet_index       = 111
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name        = "tf-test-server-mutable-fields-after"
    server_description = "updated server"
    bandwidth          = 400
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_nameSetDescriptionOmittedAndBandwidth300_serverCreated.server_id &&
      viettelcloud_server.server.name == "tf-test-server-mutable-fields-after" &&
      viettelcloud_server.server.description == "updated server" &&
      viettelcloud_server.server.bandwidth == 400 &&
      viettelcloud_server.server.flavor.kind == "predefined" &&
      viettelcloud_server.server.flavor.name == "b1.micro2x" &&
      viettelcloud_server.server.flavor.vcpus == null &&
      viettelcloud_server.server.flavor.ram == null &&
      viettelcloud_server.server.private_ips[0].id == run.createServer_nameSetDescriptionOmittedAndBandwidth300_serverCreated.private_ip_id &&
      length(viettelcloud_server.server.data_volume_ids) == 1
    )
    error_message = "Server name, description, or bandwidth was not updated in place."
  }
}

run "omitDescription_serverDescriptionSet_descriptionPreservedInPlace" {
  command   = apply
  parallel  = true
  state_key = "mutable_fields"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix        = "tf-test-server-mutable-fields"
    subnet_index       = 111
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name        = "tf-test-server-mutable-fields-after"
    server_description = null
    bandwidth          = 400
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_nameSetDescriptionOmittedAndBandwidth300_serverCreated.server_id &&
      viettelcloud_server.server.description == "updated server"
    )
    error_message = "Omitting description changed the backend value or replaced the server."
  }
}

run "clearDescriptionAndSetUntrimmedName_serverDescriptionSet_emptyDescriptionAndNamePreserved" {
  command   = apply
  parallel  = true
  state_key = "mutable_fields"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix        = "tf-test-server-mutable-fields"
    subnet_index       = 111
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name        = "  tf-test-server-mutable-fields-after  "
    server_description = ""
    bandwidth          = 400
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_nameSetDescriptionOmittedAndBandwidth300_serverCreated.server_id &&
      viettelcloud_server.server.name == "  tf-test-server-mutable-fields-after  " &&
      viettelcloud_server.server.description == ""
    )
    error_message = "Clearing description or preserving the configured name representation failed."
  }
}

run "reapply_serverWithClearedDescriptionAndUntrimmedName_identityAndStatePreserved" {
  command   = apply
  parallel  = true
  state_key = "mutable_fields"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix        = "tf-test-server-mutable-fields"
    subnet_index       = 111
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name        = "  tf-test-server-mutable-fields-after  "
    server_description = ""
    bandwidth          = 400
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_nameSetDescriptionOmittedAndBandwidth300_serverCreated.server_id &&
      viettelcloud_server.server.key_pair_id == var.key_pair_id &&
      viettelcloud_server.server.placement_group_id == var.placement_group_id &&
      (var.security_group_ids == null ? true : length(setsubtract(var.security_group_ids, viettelcloud_server.server.security_group_ids)) == 0) &&
      viettelcloud_server.server.private_ips[0].id == run.createServer_nameSetDescriptionOmittedAndBandwidth300_serverCreated.private_ip_id &&
      viettelcloud_server.server.name == "  tf-test-server-mutable-fields-after  " &&
      viettelcloud_server.server.description == "" &&
      viettelcloud_server.server.bandwidth == 400 &&
      viettelcloud_server.server.flavor.kind == "predefined" &&
      viettelcloud_server.server.flavor.name == "b1.micro2x" &&
      viettelcloud_server.server.flavor.vcpus == null &&
      viettelcloud_server.server.flavor.ram == null &&
      viettelcloud_server.server.boot.image == "cirros" &&
      viettelcloud_server.server.boot.volume_type == "Ceph_HDD_1A_10K" &&
      viettelcloud_server.server.boot.volume_size == 30
    )
    error_message = "Reapplying equivalent configuration changed server identity or managed state."
  }
}


# Chain bandwidth.

run "createServer_bandwidth400_serverCreated" {
  command   = apply
  parallel  = true
  state_key = "bandwidth"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    bandwidth          = 400
    name_prefix        = "tf-test-server-bandwidth"
    subnet_index       = 121
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name        = "  tf-test-server-bandwidth-after  "
    server_description = ""
  }

  assert {
    condition     = length(viettelcloud_server.server.id) > 0 && length(viettelcloud_server.server.private_ips) == 1 && length(viettelcloud_server.server.data_volume_ids) == 1
    error_message = "The bandwidth chain's prerequisite server was not created."
  }
}

run "changeBandwidth_serverWithBandwidth400_updatedInPlace" {
  command   = apply
  parallel  = true
  state_key = "bandwidth"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix        = "tf-test-server-bandwidth"
    subnet_index       = 121
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name        = "  tf-test-server-bandwidth-after  "
    server_description = ""
    bandwidth          = 500
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_bandwidth400_serverCreated.server_id &&
      viettelcloud_server.server.bandwidth == 500 &&
      viettelcloud_server.server.flavor.kind == "predefined" &&
      viettelcloud_server.server.flavor.name == "b1.micro2x" &&
      viettelcloud_server.server.flavor.vcpus == null &&
      viettelcloud_server.server.flavor.ram == null &&
      viettelcloud_server.server.private_ips[0].id == run.createServer_bandwidth400_serverCreated.private_ip_id
    )
    error_message = "Changing only bandwidth did not update in place."
  }
}


# Chain private_ip.

run "createServer_singleSubnetAttachment_privateIpAllocated" {
  command   = apply
  parallel  = true
  state_key = "private_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-private-ip"
    subnet_index = 131
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition = (
      length(viettelcloud_server.server.id) > 0 &&
      viettelcloud_server.server.name == "tf-test-server-private-ip-server" &&
      length(viettelcloud_server.server.private_ips) == 1 &&
      length(viettelcloud_server.server.private_ips[0].id) > 0 &&
      length(viettelcloud_server.server.private_ips[0].ip_address) > 0 &&
      viettelcloud_server.server.private_ips[0].kind == "subnet" &&
      viettelcloud_server.server.private_ips[0].delete_on_termination &&
      length(viettelcloud_server.server.data_volume_ids) == 1
    )
    error_message = "Server was not created with a single allocated subnet private IP."
  }
}

run "attachSecondSubnet_oneSubnetAttachment_twoPrivateIpsWithFirstPreserved" {
  command   = apply
  parallel  = true
  state_key = "private_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix     = "tf-test-server-private-ip"
    subnet_index    = 131
    vpc_id          = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    private_ip_mode = "both"
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_singleSubnetAttachment_privateIpAllocated.server_id &&
      length(viettelcloud_server.server.private_ips) == 2 &&
      viettelcloud_server.server.private_ips[0].id == run.createServer_singleSubnetAttachment_privateIpAllocated.private_ip_id &&
      length(viettelcloud_server.server.private_ips[1].id) > 0 &&
      length(viettelcloud_server.server.private_ips[1].ip_address) > 0
    )
    error_message = "Adding a second subnet attachment did not extend the private IP list in place."
  }
}

run "reverseOrder_twoSubnetAttachments_privateIpIdsReordered" {
  command   = apply
  parallel  = true
  state_key = "private_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix         = "tf-test-server-private-ip"
    subnet_index        = 131
    vpc_id              = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    private_ip_mode     = "both"
    reverse_private_ips = true
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_singleSubnetAttachment_privateIpAllocated.server_id &&
      length(viettelcloud_server.server.private_ips) == 2 &&
      viettelcloud_server.server.private_ips[0].id == run.attachSecondSubnet_oneSubnetAttachment_twoPrivateIpsWithFirstPreserved.private_ip_ids[1] &&
      viettelcloud_server.server.private_ips[1].id == run.attachSecondSubnet_oneSubnetAttachment_twoPrivateIpsWithFirstPreserved.private_ip_ids[0]
    )
    error_message = "Reordering the attachment list reallocated private IPs instead of reordering state."
  }
}

run "removeFirstSubnet_twoSubnetAttachments_secondPrivateIpPreserved" {
  command   = apply
  parallel  = true
  state_key = "private_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix     = "tf-test-server-private-ip"
    subnet_index    = 131
    vpc_id          = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    private_ip_mode = "second"
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_singleSubnetAttachment_privateIpAllocated.server_id &&
      length(viettelcloud_server.server.private_ips) == 1 &&
      viettelcloud_server.server.private_ips[0].id == run.attachSecondSubnet_oneSubnetAttachment_twoPrivateIpsWithFirstPreserved.private_ip_ids[1]
    )
    error_message = "Removing the first subnet attachment detached the wrong private IP."
  }
}

run "returnToFirstSubnet_secondSubnetAttachment_privateIpReallocated" {
  command   = apply
  parallel  = true
  state_key = "private_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    keep_second_subnet = true
    name_prefix        = "tf-test-server-private-ip"
    subnet_index       = 131
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_singleSubnetAttachment_privateIpAllocated.server_id &&
      length(viettelcloud_server.server.private_ips) == 1 &&
      viettelcloud_server.server.private_ips[0].id != run.removeFirstSubnet_twoSubnetAttachments_secondPrivateIpPreserved.private_ip_id &&
      length(viettelcloud_server.server.private_ips[0].ip_address) > 0
    )
    error_message = "Returning to the first subnet did not reallocate and clean up the private IP."
  }
}

run "deleteSubnet_detachedSecondSubnet_subnetRemovedAndServerPreserved" {
  command   = apply
  parallel  = true
  state_key = "private_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    keep_second_subnet = false
    name_prefix        = "tf-test-server-private-ip"
    subnet_index       = 131
    vpc_id             = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition     = length(viettelcloud_subnet.second) == 0 && viettelcloud_server.server.id == run.returnToFirstSubnet_secondSubnetAttachment_privateIpReallocated.server_id && viettelcloud_server.server.private_ips[0].id == run.returnToFirstSubnet_secondSubnetAttachment_privateIpReallocated.private_ip_id
    error_message = "The detached private IP leaked or deleting its subnet changed the server."
  }
}


# Chain existing_ip.

run "createServer_singleSubnetIpBeforeManagedIpAttach_serverCreated" {
  command   = apply
  parallel  = true
  state_key = "existing_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-existing-ip"
    subnet_index = 141
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition     = length(viettelcloud_server.server.id) > 0 && length(viettelcloud_server.server.private_ips) == 1 && length(viettelcloud_server.server.data_volume_ids) == 1
    error_message = "The existing_ip chain's prerequisite server was not created."
  }
}

run "attachManagedPrivateIp_serverWithSubnetIp_twoAttachmentsWithManagedIpPreserved" {
  command   = apply
  parallel  = true
  state_key = "existing_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix               = "tf-test-server-existing-ip"
    subnet_index              = 141
    vpc_id                    = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    create_managed_private_ip = true
    attach_managed_private_ip = true
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_singleSubnetIpBeforeManagedIpAttach_serverCreated.server_id &&
      length(viettelcloud_server.server.private_ips) == 2 &&
      viettelcloud_server.server.private_ips[0].id == run.createServer_singleSubnetIpBeforeManagedIpAttach_serverCreated.private_ip_id &&
      viettelcloud_server.server.private_ips[1].id == viettelcloud_private_ip.managed[0].id &&
      viettelcloud_server.server.private_ips[1].kind == "ip" &&
      viettelcloud_server.server.private_ips[1].delete_on_termination == false
    )
    error_message = "Attaching an existing private IP by ID did not extend the attachment list."
  }
}

run "detachManagedPrivateIp_managedIpAttachment_managedIpPreserved" {
  command   = apply
  parallel  = true
  state_key = "existing_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix               = "tf-test-server-existing-ip"
    subnet_index              = 141
    vpc_id                    = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    create_managed_private_ip = true
    attach_managed_private_ip = false
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_singleSubnetIpBeforeManagedIpAttach_serverCreated.server_id &&
      length(viettelcloud_server.server.private_ips) == 1 &&
      viettelcloud_server.server.private_ips[0].id == run.attachManagedPrivateIp_serverWithSubnetIp_twoAttachmentsWithManagedIpPreserved.private_ip_id &&
      viettelcloud_private_ip.managed[0].id == run.attachManagedPrivateIp_serverWithSubnetIp_twoAttachmentsWithManagedIpPreserved.managed_private_ip_id
    )
    error_message = "Detaching a practitioner-owned private IP removed the wrong attachment or deleted the IP."
  }
}


# Chain elastic_ip.

run "createServer_noElasticIpConfig_serverRunningWithoutElasticIps" {
  command   = apply
  parallel  = true
  state_key = "elastic_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-elastic-ip"
    subnet_index = 151
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition = (
      length(viettelcloud_server.server.id) > 0 &&
      viettelcloud_server.server.power_state == "running" &&
      length(viettelcloud_server.server.elastic_ips) == 0 &&
      length(viettelcloud_server.server.private_ips) == 1
    )
    error_message = "Server was not created running and without an elastic IP."
  }
}

run "attachElasticIp_serverWithoutElasticIps_allocatedAddressReturned" {
  command   = apply
  parallel  = true
  state_key = "elastic_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix      = "tf-test-server-elastic-ip"
    subnet_index     = 151
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    elastic_ip_count = 1
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_noElasticIpConfig_serverRunningWithoutElasticIps.server_id &&
      length(viettelcloud_server.server.elastic_ips) == 1 &&
      viettelcloud_server.server.elastic_ips[0].kind == "new" &&
      viettelcloud_server.server.elastic_ips[0].delete_on_termination &&
      length(viettelcloud_server.server.elastic_ips[0].id) > 0 &&
      length(viettelcloud_server.server.elastic_ips[0].ip_address) > 0 &&
      length(viettelcloud_server.server.elastic_ips[0].status) > 0 &&
      length(viettelcloud_server.server.private_ips) == 1
    )
    error_message = "Attaching a new elastic IP did not expose its allocated address."
  }
}

run "attachSecondElasticIp_oneElasticIpAttachment_firstAttachmentPreserved" {
  command   = apply
  parallel  = true
  state_key = "elastic_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix      = "tf-test-server-elastic-ip"
    subnet_index     = 151
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    elastic_ip_count = 2
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_noElasticIpConfig_serverRunningWithoutElasticIps.server_id &&
      length(viettelcloud_server.server.elastic_ips) == 2 &&
      viettelcloud_server.server.elastic_ips[0].id == run.attachElasticIp_serverWithoutElasticIps_allocatedAddressReturned.elastic_ip_ids[0] &&
      length(viettelcloud_server.server.elastic_ips[1].ip_address) > 0
    )
    error_message = "Adding a second elastic IP did not preserve the first attachment."
  }
}

run "removeSecondElasticIp_twoElasticIpAttachments_firstAttachmentPreserved" {
  command   = apply
  parallel  = true
  state_key = "elastic_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix      = "tf-test-server-elastic-ip"
    subnet_index     = 151
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    elastic_ip_count = 1
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_noElasticIpConfig_serverRunningWithoutElasticIps.server_id &&
      length(viettelcloud_server.server.elastic_ips) == 1 &&
      viettelcloud_server.server.elastic_ips[0].id == run.attachSecondElasticIp_oneElasticIpAttachment_firstAttachmentPreserved.elastic_ip_ids[0]
    )
    error_message = "Removing one elastic IP detached the wrong attachment."
  }
}

run "setEmptyElasticIpList_twoElasticIpAttachments_elasticIpsDetachedAndPrivateIpPreserved" {
  command   = apply
  parallel  = true
  state_key = "elastic_ip"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix      = "tf-test-server-elastic-ip"
    subnet_index     = 151
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    elastic_ip_count = 0
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_noElasticIpConfig_serverRunningWithoutElasticIps.server_id &&
      length(viettelcloud_server.server.elastic_ips) == 0 &&
      length(viettelcloud_server.server.private_ips) == 1
    )
    error_message = "An empty elastic IP list did not detach every elastic IP."
  }
}


# Chain power.

run "createServer_powerStateOmitted_serverCreated" {
  command   = apply
  parallel  = true
  state_key = "power"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-power"
    subnet_index = 161
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition     = length(viettelcloud_server.server.id) > 0 && length(viettelcloud_server.server.private_ips) == 1 && length(viettelcloud_server.server.data_volume_ids) == 1
    error_message = "The power chain's prerequisite server was not created."
  }
}

run "requestShutdown_runningServer_serverStoppedInPlace" {
  command   = apply
  parallel  = true
  state_key = "power"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-power"
    subnet_index = 161
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    power_state  = "shutdown"
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_powerStateOmitted_serverCreated.server_id &&
      viettelcloud_server.server.power_state == "shutdown"
    )
    error_message = "Requesting the shutdown power state did not stop the server in place."
  }
}

run "planAgain_stoppedServer_powerStateStable" {
  command   = plan
  parallel  = true
  state_key = "power"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-power"
    subnet_index = 161
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    power_state  = "shutdown"
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_powerStateOmitted_serverCreated.server_id &&
      viettelcloud_server.server.power_state == "shutdown"
    )
    error_message = "Replanning a stopped server reported power state drift."
  }
}

run "requestRunning_stoppedServer_serverStartedInPlace" {
  command   = apply
  parallel  = true
  state_key = "power"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-power"
    subnet_index = 161
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    power_state  = "running"
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_powerStateOmitted_serverCreated.server_id &&
      viettelcloud_server.server.power_state == "running" &&
      viettelcloud_server.server.flavor.kind == "predefined" &&
      viettelcloud_server.server.flavor.name == "b1.micro2x"
    )
    error_message = "Requesting the running power state did not start the server in place."
  }
}


# Chain rebuild.

run "createServer_predefinedFlavorAndBootImage_configuredFlavorAndImageReturned" {
  command   = apply
  parallel  = true
  state_key = "rebuild"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-rebuild"
    subnet_index = 171
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition = (
      length(viettelcloud_server.server.id) > 0 &&
      viettelcloud_server.server.boot.image == "cirros" &&
      viettelcloud_server.server.flavor.kind == "predefined" &&
      viettelcloud_server.server.flavor.name == "b1.micro2x" &&
      length(viettelcloud_server.server.private_ips) == 1 &&
      length(viettelcloud_server.server.data_volume_ids) == 1
    )
    error_message = "Server was not created with the expected boot image and predefined flavor."
  }
}

run "changeImage_existingBootImage_serverRebuiltInPlace" {
  command   = apply
  parallel  = true
  state_key = "rebuild"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-rebuild"
    subnet_index = 171
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    image        = "ubuntu-20.04-publiccloud"
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_predefinedFlavorAndBootImage_configuredFlavorAndImageReturned.server_id &&
      viettelcloud_server.server.boot.image == "ubuntu-20.04-publiccloud" &&
      viettelcloud_server.server.power_state == "running" &&
      viettelcloud_server.server.private_ips[0].id == run.createServer_predefinedFlavorAndBootImage_configuredFlavorAndImageReturned.private_ip_id
    )
    error_message = "Changing the boot image did not rebuild the server in place."
  }
}

run "restoreOriginalImage_rebuiltServer_serverRebuiltInPlace" {
  command   = apply
  parallel  = true
  state_key = "rebuild"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-rebuild"
    subnet_index = 171
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    image        = "cirros"
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_predefinedFlavorAndBootImage_configuredFlavorAndImageReturned.server_id &&
      viettelcloud_server.server.boot.image == "cirros" &&
      viettelcloud_server.server.power_state == "running" &&
      viettelcloud_server.server.private_ips[0].id == run.createServer_predefinedFlavorAndBootImage_configuredFlavorAndImageReturned.private_ip_id
    )
    error_message = "Rebuilding back to the original image did not succeed in place."
  }
}

run "rebuildAndResizeRunningServer_newFlavorAndImage_rebuiltAndResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "rebuild"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-rebuild"
    subnet_index = 171
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    image        = "ubuntu-20.04-publiccloud"
    flavor_kind  = "custom"
    flavor_vcpus = 2
    flavor_ram   = 4
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_predefinedFlavorAndBootImage_configuredFlavorAndImageReturned.server_id &&
      viettelcloud_server.server.boot.image == "ubuntu-20.04-publiccloud" &&
      viettelcloud_server.server.flavor.kind == "custom" &&
      viettelcloud_server.server.flavor.vcpus == 2 &&
      viettelcloud_server.server.flavor.ram == 4 &&
      viettelcloud_server.server.power_state == "running" &&
      viettelcloud_server.server.private_ips[0].id == run.createServer_predefinedFlavorAndBootImage_configuredFlavorAndImageReturned.private_ip_id
    )
    error_message = "Simultaneous rebuild and resize on a running server did not update both boot image and flavor in place."
  }
}


# Chain user_data.

run "createServer_userDataOmitted_serverCreated" {
  command   = apply
  parallel  = true
  state_key = "user_data"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-user-data"
    subnet_index = 181
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition     = length(viettelcloud_server.server.id) > 0 && length(viettelcloud_server.server.private_ips) == 1 && length(viettelcloud_server.server.data_volume_ids) == 1
    error_message = "The user_data chain's prerequisite server was not created."
  }
}

run "configureUserData_serverWithoutUserData_serverAndPrivateIpReplaced" {
  command   = apply
  parallel  = true
  state_key = "user_data"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-user-data"
    subnet_index = 181
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    user_data    = "#cloud-config\npackage_update: true\n"
  }

  assert {
    condition = (
      viettelcloud_server.server.id != run.createServer_userDataOmitted_serverCreated.server_id &&
      length(viettelcloud_server.server.private_ips) == 1 &&
      viettelcloud_server.server.private_ips[0].id != run.createServer_userDataOmitted_serverCreated.private_ip_id &&
      length(viettelcloud_server.server.data_volume_ids) == 1
    )
    error_message = "Configuring user data did not replace the server."
  }
}

run "planAgain_configuredUserData_serverIdentityPreserved" {
  command   = plan
  parallel  = true
  state_key = "user_data"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-user-data"
    subnet_index = 181
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    user_data    = "#cloud-config\npackage_update: true\n"
  }

  assert {
    condition     = viettelcloud_server.server.id == run.configureUserData_serverWithoutUserData_serverAndPrivateIpReplaced.server_id
    error_message = "Configured user data planned another replacement instead of settling."
  }
}


# Chain data_volume.

run "createServer_dataVolumeSize20_serverCreated" {
  command   = apply
  parallel  = true
  state_key = "data_volume"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    user_data               = "#cloud-config\npackage_update: true\n"
    name_prefix             = "tf-test-server-data-volume"
    subnet_index            = 191
    vpc_id                  = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    second_data_volume_size = 20
  }

  # Two data volumes of different sizes: a single-volume list cannot show that
  # each element resolved to its own backend volume.
  assert {
    condition = (
      length(viettelcloud_server.server.id) > 0 &&
      length(viettelcloud_server.server.private_ips) == 1 &&
      length(viettelcloud_server.server.data_volume_ids) == 2 &&
      viettelcloud_server.server.data_volumes[0].volume_size == 20 &&
      viettelcloud_server.server.data_volumes[1].volume_size == 20 &&
      length(viettelcloud_server.server.data_volumes[0].id) > 0 &&
      viettelcloud_server.server.data_volumes[0].id != viettelcloud_server.server.data_volumes[1].id
    )
    error_message = "The data_volume chain's prerequisite server was not created with two distinct data volumes."
  }
}

# Boot alone. Every data volume keeping its size proves a boot resize does not
# reach the data volumes sharing the same update.
run "increaseBootVolumeSize_existingServer_onlyBootVolumeResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "data_volume"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix             = "tf-test-server-data-volume"
    subnet_index            = 191
    vpc_id                  = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    user_data               = "#cloud-config\npackage_update: true\n"
    boot_volume_size        = 40
    second_data_volume_size = 20
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_dataVolumeSize20_serverCreated.server_id &&
      viettelcloud_server.server.boot.volume_size == 40 &&
      viettelcloud_server.server.data_volumes[0].volume_size == 20 &&
      viettelcloud_server.server.data_volumes[1].volume_size == 20 &&
      viettelcloud_server.server.data_volumes[*].id == run.createServer_dataVolumeSize20_serverCreated.data_volume_element_ids
    )
    error_message = "Increasing only the boot volume size replaced the server or changed a data volume."
  }
}

# One data volume alone. data_volumes[0] keeping both its ID and its size
# proves the resize followed data_volumes[1].id rather than a position in
# whatever order the server reported its attachments, and the boot volume
# holding at 40 proves a data resize does not reach it.
run "increaseDataVolumeSize_existingServer_onlySecondDataVolumeResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "data_volume"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix             = "tf-test-server-data-volume"
    subnet_index            = 191
    vpc_id                  = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    user_data               = "#cloud-config\npackage_update: true\n"
    boot_volume_size        = 40
    second_data_volume_size = 30
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_dataVolumeSize20_serverCreated.server_id &&
      viettelcloud_server.server.boot.volume_size == 40 &&
      viettelcloud_server.server.data_volumes[0].volume_size == 20 &&
      viettelcloud_server.server.data_volumes[1].volume_size == 30 &&
      viettelcloud_server.server.data_volumes[*].id == run.createServer_dataVolumeSize20_serverCreated.data_volume_element_ids
    )
    error_message = "Increasing only the second data volume size replaced the server or resized the wrong volume."
  }
}

# Boot and both data volumes in one apply, which is the case that orders the
# extends against each other rather than one at a time.
run "increaseBootAndDataVolumeSizes_existingServer_allVolumesResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "data_volume"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix             = "tf-test-server-data-volume"
    subnet_index            = 191
    vpc_id                  = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    user_data               = "#cloud-config\npackage_update: true\n"
    boot_volume_size        = 50
    data_volume_size        = 30
    second_data_volume_size = 40
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_dataVolumeSize20_serverCreated.server_id &&
      viettelcloud_server.server.boot.volume_size == 50 &&
      viettelcloud_server.server.data_volumes[0].volume_size == 30 &&
      viettelcloud_server.server.data_volumes[1].volume_size == 40 &&
      viettelcloud_server.server.data_volumes[*].id == run.createServer_dataVolumeSize20_serverCreated.data_volume_element_ids
    )
    error_message = "Increasing the boot and both data volume sizes together replaced the server or missed a volume."
  }
}

run "clearDeleteOnTermination_volumesDeletedOnTermination_volumesKeptInPlace" {
  command   = apply
  parallel  = true
  state_key = "data_volume"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix                       = "tf-test-server-data-volume"
    subnet_index                      = 191
    vpc_id                            = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    user_data                         = "#cloud-config\npackage_update: true\n"
    boot_volume_size                  = 50
    data_volume_size                  = 30
    second_data_volume_size           = 40
    boot_delete_on_termination        = false
    data_volume_delete_on_termination = false
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_dataVolumeSize20_serverCreated.server_id &&
      viettelcloud_server.server.data_volume_ids == run.createServer_dataVolumeSize20_serverCreated.data_volume_ids &&
      viettelcloud_server.server.boot.delete_on_termination == false &&
      viettelcloud_server.server.data_volumes[0].delete_on_termination == false &&
      viettelcloud_server.server.data_volumes[1].delete_on_termination == false
    )
    error_message = "Opting the boot and data volumes out of deletion did not update the server in place."
  }
}

# delete_on_termination is optional and computed, so removing it from
# configuration preserves the last applied value instead of returning to the
# default. A practitioner who wants the volumes deleted again sets it back.
run "omitDeleteOnTermination_volumesKeptOnTermination_keptValuePreserved" {
  command   = apply
  parallel  = true
  state_key = "data_volume"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix             = "tf-test-server-data-volume"
    subnet_index            = 191
    vpc_id                  = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    user_data               = "#cloud-config\npackage_update: true\n"
    boot_volume_size        = 50
    data_volume_size        = 30
    second_data_volume_size = 40
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_dataVolumeSize20_serverCreated.server_id &&
      viettelcloud_server.server.boot.delete_on_termination == false &&
      viettelcloud_server.server.data_volumes[0].delete_on_termination == false &&
      viettelcloud_server.server.data_volumes[1].delete_on_termination == false
    )
    error_message = "Omitting delete_on_termination did not preserve the configured value."
  }
}

# Restores the default behavior so teardown deletes both volumes with the
# server instead of leaving them behind in the test project.
run "restoreDeleteOnTermination_volumesKeptOnTermination_volumesDeletedInPlace" {
  command   = apply
  parallel  = true
  state_key = "data_volume"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix                       = "tf-test-server-data-volume"
    subnet_index                      = 191
    vpc_id                            = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    user_data                         = "#cloud-config\npackage_update: true\n"
    boot_volume_size                  = 50
    data_volume_size                  = 30
    second_data_volume_size           = 40
    boot_delete_on_termination        = true
    data_volume_delete_on_termination = true
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_dataVolumeSize20_serverCreated.server_id &&
      viettelcloud_server.server.boot.delete_on_termination &&
      viettelcloud_server.server.data_volumes[0].delete_on_termination &&
      viettelcloud_server.server.data_volumes[1].delete_on_termination
    )
    error_message = "Opting the boot and data volumes back into deletion did not update the server in place."
  }
}


# Chain to_custom.

run "createServer_predefinedFlavorForCustomSwitch_serverCreated" {
  command   = apply
  parallel  = true
  state_key = "to_custom"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    data_volume_size = 30
    user_data        = "#cloud-config\npackage_update: true\n"
    name_prefix      = "tf-test-server-to-custom"
    subnet_index     = 201
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition     = length(viettelcloud_server.server.id) > 0 && length(viettelcloud_server.server.private_ips) == 1 && length(viettelcloud_server.server.data_volume_ids) == 1
    error_message = "The to_custom chain's prerequisite server was not created."
  }
}

run "switchToCustom_predefinedFlavor_serverResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "to_custom"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix      = "tf-test-server-to-custom"
    subnet_index     = 201
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    flavor_kind      = "custom"
    flavor_vcpus     = 2
    flavor_ram       = 4
    user_data        = "#cloud-config\npackage_update: true\n"
    data_volume_size = 30
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_predefinedFlavorForCustomSwitch_serverCreated.server_id &&
      viettelcloud_server.server.flavor.kind == "custom" &&
      viettelcloud_server.server.flavor.name == null &&
      viettelcloud_server.server.flavor.vcpus == 2 &&
      viettelcloud_server.server.flavor.ram == 4 &&
      viettelcloud_server.server.power_state == "running"
    )
    error_message = "Switching to a custom flavor did not resize the server in place."
  }
}


# Chain custom_resize.

run "createServer_predefinedFlavorForCapacityResize_serverCreated" {
  command   = apply
  parallel  = true
  state_key = "custom_resize"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    flavor_ram       = 4
    flavor_vcpus     = 2
    flavor_kind      = "predefined"
    data_volume_size = 30
    user_data        = "#cloud-config\npackage_update: true\n"
    name_prefix      = "tf-test-server-custom-resize"
    subnet_index     = 211
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition     = length(viettelcloud_server.server.id) > 0 && length(viettelcloud_server.server.private_ips) == 1 && length(viettelcloud_server.server.data_volume_ids) == 1
    error_message = "The custom_resize chain's prerequisite server was not created."
  }
}

# Reach the prior custom capacity through the original predefined-to-custom lifecycle.
run "switchToCustom_predefinedFlavorForCapacityResize_serverResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "custom_resize"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix      = "tf-test-server-custom-resize"
    subnet_index     = 211
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    flavor_kind      = "custom"
    flavor_vcpus     = 2
    flavor_ram       = 4
    user_data        = "#cloud-config\npackage_update: true\n"
    data_volume_size = 30
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_predefinedFlavorForCapacityResize_serverCreated.server_id &&
      viettelcloud_server.server.flavor.kind == "custom" &&
      viettelcloud_server.server.flavor.name == null &&
      viettelcloud_server.server.flavor.vcpus == 2 &&
      viettelcloud_server.server.flavor.ram == 4 &&
      viettelcloud_server.server.power_state == "running"
    )
    error_message = "Switching to a custom flavor did not resize the server in place."
  }
}

run "changeCapacity_customFlavor_serverResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "custom_resize"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix      = "tf-test-server-custom-resize"
    subnet_index     = 211
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    flavor_kind      = "custom"
    flavor_vcpus     = 4
    flavor_ram       = 4
    user_data        = "#cloud-config\npackage_update: true\n"
    data_volume_size = 30
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_predefinedFlavorForCapacityResize_serverCreated.server_id &&
      viettelcloud_server.server.flavor.kind == "custom" &&
      viettelcloud_server.server.flavor.name == null &&
      viettelcloud_server.server.flavor.vcpus == 4 &&
      viettelcloud_server.server.flavor.ram == 4 &&
      viettelcloud_server.server.power_state == "running"
    )
    error_message = "Resizing custom flavor capacity did not update the server in place."
  }
}


# Chain to_predefined.

run "createServer_predefinedFlavorForRoundTripResize_serverCreated" {
  command   = apply
  parallel  = true
  state_key = "to_predefined"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    flavor_ram       = 4
    flavor_vcpus     = 4
    flavor_kind      = "predefined"
    data_volume_size = 30
    user_data        = "#cloud-config\npackage_update: true\n"
    name_prefix      = "tf-test-server-to-predefined"
    subnet_index     = 221
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
  }

  assert {
    condition     = length(viettelcloud_server.server.id) > 0 && length(viettelcloud_server.server.private_ips) == 1 && length(viettelcloud_server.server.data_volume_ids) == 1
    error_message = "The to_predefined chain's prerequisite server was not created."
  }
}

# Reach the prior custom capacity through the original predefined-to-custom lifecycle.
run "switchToCustom_predefinedFlavorForRoundTripResize_serverResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "to_predefined"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix      = "tf-test-server-to-predefined"
    subnet_index     = 221
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    flavor_kind      = "custom"
    flavor_vcpus     = 2
    flavor_ram       = 4
    user_data        = "#cloud-config\npackage_update: true\n"
    data_volume_size = 30
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_predefinedFlavorForRoundTripResize_serverCreated.server_id &&
      viettelcloud_server.server.flavor.kind == "custom" &&
      viettelcloud_server.server.flavor.name == null &&
      viettelcloud_server.server.flavor.vcpus == 2 &&
      viettelcloud_server.server.flavor.ram == 4 &&
      viettelcloud_server.server.power_state == "running"
    )
    error_message = "Switching to a custom flavor did not resize the server in place."
  }
}

run "changeCapacity_customFlavorBeforePredefinedSwitch_serverResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "to_predefined"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix      = "tf-test-server-to-predefined"
    subnet_index     = 221
    vpc_id           = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    flavor_kind      = "custom"
    flavor_vcpus     = 4
    flavor_ram       = 4
    user_data        = "#cloud-config\npackage_update: true\n"
    data_volume_size = 30
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_predefinedFlavorForRoundTripResize_serverCreated.server_id &&
      viettelcloud_server.server.flavor.kind == "custom" &&
      viettelcloud_server.server.flavor.name == null &&
      viettelcloud_server.server.flavor.vcpus == 4 &&
      viettelcloud_server.server.flavor.ram == 4 &&
      viettelcloud_server.server.power_state == "running"
    )
    error_message = "Resizing custom flavor capacity did not update the server in place."
  }
}

run "switchToPredefined_resizedCustomFlavor_serverResizedInPlace" {
  command   = apply
  parallel  = true
  state_key = "to_predefined"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix            = "tf-test-server-to-predefined"
    subnet_index           = 221
    vpc_id                 = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    flavor_kind            = "predefined"
    predefined_flavor_name = "b1.micro2x"
    user_data              = "#cloud-config\npackage_update: true\n"
    data_volume_size       = 30
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_predefinedFlavorForRoundTripResize_serverCreated.server_id &&
      viettelcloud_server.server.flavor.kind == "predefined" &&
      viettelcloud_server.server.flavor.name == "b1.micro2x" &&
      viettelcloud_server.server.flavor.vcpus == null &&
      viettelcloud_server.server.flavor.ram == null &&
      viettelcloud_server.server.power_state == "running"
    )
    error_message = "Switching back to predefined flavor did not resize the server in place."
  }
}


# Chain import.

run "createServer_importFixtureWithVolumeAndPrivateIp_serverWithVolumeAndPrivateIpReturned" {
  command   = apply
  parallel  = true
  state_key = "import"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-import"
    subnet_index = 231
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    server_name  = "tf-test-server-import"
  }

  assert {
    condition = (
      length(viettelcloud_server.server.id) > 0 &&
      length(viettelcloud_server.server.data_volume_ids) == 1 &&
      length(viettelcloud_server.server.private_ips) == 1
    )
    error_message = "The import chain's server was not created with a data volume and a private IP."
  }
}

run "planImport_existingServerId_runtimeAndAttachmentStateRestored" {
  command   = plan
  parallel  = true
  state_key = "import_target"

  module {
    source = "./fixtures/server/import"
  }

  variables {
    server_id   = run.createServer_importFixtureWithVolumeAndPrivateIp_serverWithVolumeAndPrivateIpReturned.server_id
    server_name = "tf-test-server-import"
  }

  assert {
    condition = (
      viettelcloud_server.imported.id == run.createServer_importFixtureWithVolumeAndPrivateIp_serverWithVolumeAndPrivateIpReturned.server_id &&
      viettelcloud_server.imported.name == "tf-test-server-import" &&
      viettelcloud_server.imported.zone == "vn-central-1a" &&
      viettelcloud_server.imported.quantity == 1 &&
      viettelcloud_server.imported.power_state == "running" &&
      length(viettelcloud_server.imported.status) > 0 &&
      length(viettelcloud_server.imported.server_type) > 0 &&
      length(viettelcloud_server.imported.created_at) > 0 &&
      viettelcloud_server.imported.flavor.kind == "predefined" &&
      viettelcloud_server.imported.flavor.name == "b1.micro2x" &&
      viettelcloud_server.imported.flavor.vcpus == null &&
      viettelcloud_server.imported.flavor.ram == null &&
      viettelcloud_server.imported.boot.boot_type == "image" &&
      viettelcloud_server.imported.boot.image == "cirros" &&
      viettelcloud_server.imported.boot.volume_type == "Ceph_HDD_1A_10K" &&
      viettelcloud_server.imported.boot.volume_size == 30 &&
      viettelcloud_server.imported.boot.iops != null &&
      viettelcloud_server.imported.data_volume_ids == run.createServer_importFixtureWithVolumeAndPrivateIp_serverWithVolumeAndPrivateIpReturned.data_volume_ids &&
      length(viettelcloud_server.imported.private_ips) == 1 &&
      length(viettelcloud_server.imported.private_ips[0].id) > 0 &&
      length(viettelcloud_server.imported.private_ips[0].ip_address) > 0 &&
      length(viettelcloud_server.imported.elastic_ips) == 0
    )
    error_message = "Importing the server did not reproduce its flavor, boot, data volume, and attachment state."
  }
}


# Chain offline_operations.

run "createServer_stoppedServer_serverCreatedShutdown" {
  command   = apply
  parallel  = true
  state_key = "offline_operations"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-offline-ops"
    subnet_index = 241
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    power_state  = "shutdown"
  }

  assert {
    condition = (
      length(viettelcloud_server.server.id) > 0 &&
      viettelcloud_server.server.power_state == "shutdown" &&
      viettelcloud_server.server.boot.image == "cirros" &&
      viettelcloud_server.server.flavor.kind == "predefined" &&
      viettelcloud_server.server.flavor.name == "b1.micro2x" &&
      length(viettelcloud_server.server.private_ips) == 1 &&
      length(viettelcloud_server.server.data_volume_ids) == 1
    )
    error_message = "Server was not created in shutdown power state."
  }
}

run "resizeStoppedServer_customFlavor_serverResizedInPlaceWhileShutdown" {
  command   = apply
  parallel  = true
  state_key = "offline_operations"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-offline-ops"
    subnet_index = 241
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    power_state  = "shutdown"
    flavor_kind  = "custom"
    flavor_vcpus = 2
    flavor_ram   = 4
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_stoppedServer_serverCreatedShutdown.server_id &&
      viettelcloud_server.server.flavor.kind == "custom" &&
      viettelcloud_server.server.flavor.name == null &&
      viettelcloud_server.server.flavor.vcpus == 2 &&
      viettelcloud_server.server.flavor.ram == 4 &&
      viettelcloud_server.server.power_state == "shutdown"
    )
    error_message = "Offline resize on a stopped server did not update flavor in place while remaining shutdown."
  }
}

run "rebuildStoppedServer_newImage_serverRebuiltInPlaceWhileShutdown" {
  command   = apply
  parallel  = true
  state_key = "offline_operations"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-offline-ops"
    subnet_index = 241
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    power_state  = "shutdown"
    flavor_kind  = "custom"
    flavor_vcpus = 2
    flavor_ram   = 4
    image        = "ubuntu-20.04-publiccloud"
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_stoppedServer_serverCreatedShutdown.server_id &&
      viettelcloud_server.server.boot.image == "ubuntu-20.04-publiccloud" &&
      viettelcloud_server.server.flavor.kind == "custom" &&
      viettelcloud_server.server.flavor.vcpus == 2 &&
      viettelcloud_server.server.flavor.ram == 4 &&
      viettelcloud_server.server.power_state == "shutdown"
    )
    error_message = "Offline rebuild on a stopped server did not rebuild image in place while remaining shutdown."
  }
}

run "rebuildAndStartServer_stoppedAfterOfflineOperations_serverRebuiltAndStarted" {
  command   = apply
  parallel  = true
  state_key = "offline_operations"

  module {
    source = "./fixtures/server/resource"
  }

  variables {
    name_prefix  = "tf-test-server-offline-ops"
    subnet_index = 241
    vpc_id       = run.createVpc_isolatedNetwork_validVpcIdReturned.vpc_id
    power_state  = "running"
    flavor_kind  = "custom"
    flavor_vcpus = 2
    flavor_ram   = 4
    image        = "cirros"
  }

  assert {
    condition = (
      viettelcloud_server.server.id == run.createServer_stoppedServer_serverCreatedShutdown.server_id &&
      viettelcloud_server.server.power_state == "running" &&
      viettelcloud_server.server.boot.image == "cirros" &&
      viettelcloud_server.server.flavor.kind == "custom" &&
      viettelcloud_server.server.flavor.vcpus == 2 &&
      viettelcloud_server.server.flavor.ram == 4
    )
    error_message = "Rebuilding and starting the stopped server did not complete in place."
  }
}
