# Test index — copy a run declaration below and Find Next to jump to its block.
# Chains and runs are listed in file order; setup runs are included.
#
# Chain lookup (shared fixture state):
#   run "querySupportedFilters_isolatedKeyPairs_matchingObjectsAndConfiguredValuesReturned" {
#   run "planAgain_preservedConfiguredValues_stateStable" {

provider "viettelcloud" {
  endpoint   = var.api_endpoint
  token      = var.token
  project_id = var.project_id
}

# Create two key pairs and verify every supported lookup criterion.
run "querySupportedFilters_isolatedKeyPairs_matchingObjectsAndConfiguredValuesReturned" {
  command = apply

  module {
    source = "./fixtures/key_pair/datasource"
  }

  variables {
    key_pair1_name = "tf-test-ds-kp-1"
    key_pair2_name = "tf-test-ds-kp-2"
  }

  # Assert lookup by ID returns every attribute of key pair 1.
  assert {
    condition = (
      data.viettelcloud_key_pair.by_id.id == viettelcloud_key_pair.kp1.id &&
      data.viettelcloud_key_pair.by_id.name == "tf-test-ds-kp-1" &&
      length(data.viettelcloud_key_pair.by_id.public_key) > 0 &&
      length(data.viettelcloud_key_pair.by_id.fingerprint) > 0 &&
      data.viettelcloud_key_pair.by_id.type == "rsa" &&
      length(data.viettelcloud_key_pair.by_id.created_at) > 0 &&
      length(data.viettelcloud_key_pair.by_id.updated_at) > 0
    )
    error_message = "Key pair lookup by ID returned unexpected values."
  }

  # Assert lookup by Name selects key pair 2.
  assert {
    condition = (
      data.viettelcloud_key_pair.by_name.id == viettelcloud_key_pair.kp2.id &&
      data.viettelcloud_key_pair.by_name.name == "tf-test-ds-kp-2" &&
      length(data.viettelcloud_key_pair.by_name.public_key) > 0 &&
      length(data.viettelcloud_key_pair.by_name.fingerprint) > 0 &&
      data.viettelcloud_key_pair.by_name.type == "rsa"
    )
    error_message = "Key pair lookup by Name returned unexpected values."
  }

  # Assert lookup by Fingerprint selects key pair 1.
  assert {
    condition = (
      data.viettelcloud_key_pair.by_fingerprint.id == viettelcloud_key_pair.kp1.id &&
      data.viettelcloud_key_pair.by_fingerprint.name == "tf-test-ds-kp-1" &&
      data.viettelcloud_key_pair.by_fingerprint.fingerprint == viettelcloud_key_pair.kp1.fingerprint
    )
    error_message = "Key pair lookup by Fingerprint returned unexpected values."
  }

  # Assert combined lookup with untrimmed name, uppercase fingerprint, and uppercase ID preserves configured values in state.
  assert {
    condition = (
      data.viettelcloud_key_pair.by_all_filters.id == upper(viettelcloud_key_pair.kp1.id) &&
      data.viettelcloud_key_pair.by_all_filters.name == " tf-test-ds-kp-1 " &&
      data.viettelcloud_key_pair.by_all_filters.fingerprint == upper(viettelcloud_key_pair.kp1.fingerprint) &&
      data.viettelcloud_key_pair.by_all_filters.type == "rsa"
    )
    error_message = "Key pair lookup with combined filters did not preserve configured values in state."
  }
}

# Re-plans without configuration changes to verify preserved configured values (casing and whitespace) do not drift.
run "planAgain_preservedConfiguredValues_stateStable" {
  command = plan

  module {
    source = "./fixtures/key_pair/datasource"
  }

  variables {
    key_pair1_name = "tf-test-ds-kp-1"
    key_pair2_name = "tf-test-ds-kp-2"
  }

  assert {
    condition = (
      data.viettelcloud_key_pair.by_id.id == run.querySupportedFilters_isolatedKeyPairs_matchingObjectsAndConfiguredValuesReturned.kp1_id &&
      data.viettelcloud_key_pair.by_id.name == "tf-test-ds-kp-1" &&
      data.viettelcloud_key_pair.by_name.id == run.querySupportedFilters_isolatedKeyPairs_matchingObjectsAndConfiguredValuesReturned.kp2_id &&
      data.viettelcloud_key_pair.by_name.name == "tf-test-ds-kp-2" &&
      data.viettelcloud_key_pair.by_fingerprint.id == run.querySupportedFilters_isolatedKeyPairs_matchingObjectsAndConfiguredValuesReturned.kp1_id &&
      data.viettelcloud_key_pair.by_all_filters.id == upper(run.querySupportedFilters_isolatedKeyPairs_matchingObjectsAndConfiguredValuesReturned.kp1_id) &&
      data.viettelcloud_key_pair.by_all_filters.name == " tf-test-ds-kp-1 " &&
      data.viettelcloud_key_pair.by_all_filters.fingerprint == upper(run.querySupportedFilters_isolatedKeyPairs_matchingObjectsAndConfiguredValuesReturned.kp1_fingerprint)
    )
    error_message = "Subsequent plan detected drift on data source configured values."
  }
}
