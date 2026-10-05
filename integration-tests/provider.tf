terraform {
  # 1.12 introduced the `parallel` and `state_key` run attributes the server
  # resource test uses to run its lifecycle chains concurrently.
  required_version = ">= 1.12"

  required_providers {
    viettelcloud = {
      source = "viettelcloud/viettelcloud"
    }
  }
}

variable "token" {
  description = "Viettel Cloud API access token used by the integration tests."
  type        = string
  sensitive   = true
}

variable "project_id" {
  description = "Project UUID or slug used by the integration tests."
  type        = string
}

variable "api_endpoint" {
  description = "Viettel Cloud API endpoint used by the integration tests."
  type        = string
}

variable "keypair_id" {
  description = "Key pair UUID used by server integration tests."
  type        = string
  default     = null
}

variable "placementgroup_id" {
  description = "Placement group UUID used by server integration tests."
  type        = string
  default     = null
}

variable "security_group_id" {
  description = "Security group UUID used by server integration tests."
  type        = string
  default     = null
}
