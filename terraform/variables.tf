variable "yc_token" {
  type        = string
  description = "Yandex Cloud OAuth token"
  sensitive   = true
}

variable "yc_cloud_id" {
  type        = string
  description = "Yandex Cloud ID (optional)"
  default     = ""
}

variable "yc_folder_id" {
  type        = string
  description = "Yandex Cloud folder ID"
}

variable "yc_zone" {
  type        = string
  description = "Yandex Cloud zone"
  default     = "ru-central1-a"
}

variable "ssh_user" {
  type        = string
  description = "SSH user for VM"
  default     = "ubuntu"
}

variable "ssh_public_key_path" {
  type        = string
  description = "Path to SSH public key (absolute path or relative to project)"
}
