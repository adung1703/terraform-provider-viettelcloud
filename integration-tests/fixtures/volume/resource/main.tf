terraform {
  required_version = ">= 1.12"

  required_providers {
    viettelcloud = {
      source = "viettelcloud/viettelcloud"
    }
  }
}

variable "volumes" {
  type = map(object({
    name        = string
    description = optional(string)
    # Optional because a snapshot source determines the size itself and the
    # provider rejects an explicit size when the volume is created from one.
    size = optional(number)
    iops = optional(number)
    create_from = optional(object({
      source_type     = optional(string, "empty")
      image           = optional(string)
      custom_image_id = optional(string)
      snapshot_id     = optional(string)
      backup_id       = optional(string)
      # Per-volume override of var.volume_type. The backend retypes in place, so
      # changing it for one fleet member exercises the update path without
      # touching the rest of the fleet.
      volume_type = optional(string)
    }), {})
  }))
}

variable "volume_type" {
  type = string
}

# Disambiguates volume_type when the same name exists in more than one zone.
variable "zone" {
  type    = string
  default = null
}

variable "image_name" {
  type = string
}

resource "viettelcloud_volume" "volume" {
  for_each = var.volumes

  name        = each.value.name
  description = each.value.description
  size        = each.value.size
  iops        = each.value.iops
  # A snapshot determines the new volume's zone.
  zone = each.value.create_from.source_type == "snapshot" ? null : var.zone

  create_from = {
    source_type     = each.value.create_from.source_type
    image           = each.value.create_from.image
    custom_image_id = each.value.create_from.custom_image_id
    snapshot_id     = each.value.create_from.snapshot_id
    backup_id       = each.value.create_from.backup_id
    # A snapshot carries its own volume type, so the provider rejects the
    # attribute for that source.
    volume_type = (each.value.create_from.source_type == "snapshot"
      ? null
      : coalesce(each.value.create_from.volume_type, var.volume_type)
    )
  }
}

output "volume_ids" {
  value = { for key, volume in viettelcloud_volume.volume : key => volume.id }
}

# Every attribute a later run compares against to prove a plan is empty.
# updated_at is deliberately excluded: the backend may bump it on its own.
output "volumes" {
  value = { for key, volume in viettelcloud_volume.volume : key => {
    id                     = volume.id
    name                   = volume.name
    description            = volume.description
    size                   = volume.size
    iops                   = volume.iops
    status                 = volume.status
    bootable               = volume.bootable
    encrypted              = volume.encrypted
    zone                   = volume.zone
    zone_id                = volume.zone_id
    volume_type            = volume.volume_type
    project_id             = volume.project_id
    created_at             = volume.created_at
    source_type            = volume.create_from.source_type
    source_image           = volume.create_from.image
    source_custom_image_id = volume.create_from.custom_image_id
    source_snapshot_id     = volume.create_from.snapshot_id
    source_backup_id       = volume.create_from.backup_id
    source_volume_type     = volume.create_from.volume_type
  } }
}
