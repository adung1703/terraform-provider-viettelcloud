# Lookup Elastic IP by ID
data "viettelcloud_elastic_ip" "by_id" {
  id = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}

# Lookup Elastic IP by IPv4 address
data "viettelcloud_elastic_ip" "by_ip_address" {
  ip_address = "203.0.113.10"
}

# Lookup Elastic IP by IPv6 address
data "viettelcloud_elastic_ip" "by_ipv6_address" {
  ipv6_address = "2001:db8::10"
}

# Lookup Elastic IP by address within a region
data "viettelcloud_elastic_ip" "by_ip_address_and_region" {
  ip_address = "203.0.113.10"
  region     = "vn-central-1"
}

# Lookup an active Elastic IP by address
data "viettelcloud_elastic_ip" "active_by_ip_address" {
  ip_address = "203.0.113.10"
  status     = "active"
}

# Lookup an address no server holds, e.g. before attaching it to a workload
data "viettelcloud_elastic_ip" "available_by_ip_address" {
  ip_address = "203.0.113.10"
  available  = true
}
