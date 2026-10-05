# Lookup Placement Group by ID
data "viettelcloud_placement_group" "by_id" {
  id = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}

# Lookup Placement Group by Name
data "viettelcloud_placement_group" "by_name" {
  name = "my-placement-group"
}

# Lookup Placement Group by Name and Policy
data "viettelcloud_placement_group" "by_name_and_policy" {
  name   = "my-placement-group"
  policy = "affinity"
}

# Lookup Placement Group by Region (returns the only matching Placement Group)
data "viettelcloud_placement_group" "by_region" {
  region = "vn-central-1"
}
