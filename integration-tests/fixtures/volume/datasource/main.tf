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
    description = string
    size        = number
  }))
}

variable "lookup_key" {
  type = string
}

variable "volume_type" {
  type = string
}

# Disambiguates volume_type when the same name exists in more than one zone.
variable "zone" {
  type    = string
  default = null
}

resource "viettelcloud_volume" "volume" {
  for_each = var.volumes

  name        = each.value.name
  description = each.value.description
  size        = each.value.size
  zone        = var.zone

  create_from = {
    source_type = "empty"
    volume_type = var.volume_type
  }
}

data "viettelcloud_volume" "by_id" {
  for_each = viettelcloud_volume.volume

  id = each.value.id
}

data "viettelcloud_volume" "by_name" {
  for_each = viettelcloud_volume.volume

  name = each.value.name
}

data "viettelcloud_volume" "by_size" {
  name = viettelcloud_volume.volume[var.lookup_key].name
  size = viettelcloud_volume.volume[var.lookup_key].size
}

data "viettelcloud_volume" "by_status" {
  name   = viettelcloud_volume.volume[var.lookup_key].name
  status = viettelcloud_volume.volume[var.lookup_key].status
}

data "viettelcloud_volume" "by_bootable" {
  name     = viettelcloud_volume.volume[var.lookup_key].name
  bootable = viettelcloud_volume.volume[var.lookup_key].bootable
}

data "viettelcloud_volume" "by_zone" {
  name = viettelcloud_volume.volume[var.lookup_key].name
  zone = viettelcloud_volume.volume[var.lookup_key].zone
}

data "viettelcloud_volume" "by_all_filters" {
  id       = upper(viettelcloud_volume.volume[var.lookup_key].id)
  name     = " ${viettelcloud_volume.volume[var.lookup_key].name} "
  size     = viettelcloud_volume.volume[var.lookup_key].size
  status   = " ${viettelcloud_volume.volume[var.lookup_key].status} "
  bootable = viettelcloud_volume.volume[var.lookup_key].bootable
  zone     = " ${viettelcloud_volume.volume[var.lookup_key].zone} "
}
