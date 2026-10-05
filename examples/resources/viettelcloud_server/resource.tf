resource "viettelcloud_vpc" "example" {
  name   = "server-example-vpc"
  cidr   = "10.0.0.0/16"
  region = "vn-central-1"
}

resource "viettelcloud_subnet" "example" {
  name   = "server-example-subnet"
  cidr   = "10.0.1.0/24"
  vpc_id = viettelcloud_vpc.example.id
}

resource "viettelcloud_server" "example" {
  name = "example-server"
  zone = "vn-central-1a"

  flavor = {
    kind   = "custom"
    family = "basic"
    vcpus  = 2
    ram    = 2
  }

  boot = {
    boot_type   = "image"
    image       = "ubuntu-22.04-publiccloud"
    volume_type = "Ceph_HDD_1A_10K"
    volume_size = 30
  }

  private_ips = [{
    kind      = "subnet"
    subnet_id = viettelcloud_subnet.example.id
  }]
}
