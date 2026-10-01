# Generate a new SSH key pair. Terraform stores the returned private key in
# state; protect the state because sensitive values are not encrypted.
resource "viettelcloud_key_pair" "generated" {
  name = "example-generated-key"
}

# Upload an existing SSH public key.
resource "viettelcloud_key_pair" "uploaded" {
  name       = "example-imported-key"
  public_key = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC3... user@example.com"
}
