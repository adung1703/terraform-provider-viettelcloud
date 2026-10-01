# Lookup VPC by ID
data "viettelcloud_vpc" "by_id" {
  id = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}

# Lookup VPC by Name
data "viettelcloud_vpc" "by_name" {
  name = "my-vpc"
}

# Lookup VPC by CIDR and Region Name
data "viettelcloud_vpc" "by_cidr_and_region" {
  cidr   = "10.0.0.0/16"
  region = "vn-central-1"
}
