# Lookup Security Group by ID
data "viettelcloud_security_group" "by_id" {
  id = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}

# Lookup Security Group by Name
data "viettelcloud_security_group" "by_name" {
  name = "my-security-group"
}

# Lookup Security Group by Name and Region
data "viettelcloud_security_group" "by_name_and_region" {
  name   = "my-security-group"
  region = "HN-1"
}

# Lookup Default Security Group
data "viettelcloud_security_group" "default" {
  is_default = true
}

# Lookup Default Security Group for a Region
data "viettelcloud_security_group" "default_by_region" {
  region     = "HN-1"
  is_default = true
}
