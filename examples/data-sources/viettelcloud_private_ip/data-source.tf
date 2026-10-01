# Lookup a private IP by ID
data "viettelcloud_private_ip" "by_id" {
  id = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}

# Lookup a private IP by address and a human-readable network hierarchy
data "viettelcloud_private_ip" "by_network_hierarchy" {
  ip_address  = "10.0.1.10"
  vpc_name    = "my-vpc"
  subnet_cidr = "10.0.1.0/24"
}

# Lookup a private IP by address and stable parent IDs
data "viettelcloud_private_ip" "by_parent_ids" {
  ip_address = "10.0.1.10"
  subnet_id  = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
  vpc_id     = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}
