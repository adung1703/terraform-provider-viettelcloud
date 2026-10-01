# Lookup key pair by ID
data "viettelcloud_key_pair" "by_id" {
  id = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}

# Lookup key pair by Name
data "viettelcloud_key_pair" "by_name" {
  name = "example-key"
}

# Lookup key pair by Fingerprint
data "viettelcloud_key_pair" "by_fingerprint" {
  fingerprint = "07:6c:9a:67:c1:91:57:d1:8a:98:6c:3c:0e:a2:d7:f6"
}
