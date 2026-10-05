# Lookup subnet by ID
data "viettelcloud_subnet" "by_id" {
  id = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}

# Lookup subnet by name within a VPC
data "viettelcloud_subnet" "by_name_and_vpc" {
  name     = "my-subnet"
  vpc_name = "my-vpc"
}

# Lookup subnet by CIDR and region
data "viettelcloud_subnet" "by_cidr_and_region" {
  cidr   = "10.0.1.0/24"
  region = "vn-central-1"
}
