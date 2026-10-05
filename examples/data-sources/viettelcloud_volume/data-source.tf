data "viettelcloud_volume" "by_id" {
  id = "00000000-0000-0000-0000-000000000001"
}

data "viettelcloud_volume" "by_name" {
  name = "example-data-volume"
}

data "viettelcloud_volume" "by_name_and_size" {
  name = "example-data-volume"
  size = 30
}

data "viettelcloud_volume" "by_name_and_status" {
  name   = "example-data-volume"
  status = "available"
}

data "viettelcloud_volume" "by_name_and_bootable" {
  name     = "example-data-volume"
  bootable = false
}

data "viettelcloud_volume" "by_name_and_zone" {
  name = "example-data-volume"
  zone = "zone-a"
}

data "viettelcloud_volume" "by_all_filters" {
  id       = "00000000-0000-0000-0000-000000000001"
  name     = "example-data-volume"
  size     = 30
  status   = "available"
  bootable = false
  zone     = "zone-a"
}
