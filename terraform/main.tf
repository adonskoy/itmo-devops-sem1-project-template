terraform {
  required_providers {
    yandex = {
      source  = "yandex-cloud/yandex"
      version = "~> 0.191"
    }
  }

  backend "s3" {
    key                         = "project-sem-1/terraform.tfstate"
    endpoints = {
      s3 = "https://storage.yandexcloud.net"
    }
    region                      = "ru-central1"
    skip_credentials_validation = true
    skip_metadata_api_check     = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_s3_checksum            = true
  }
}

# Аутентификация: YC_TOKEN или YC_SERVICE_ACCOUNT_KEY_FILE (путь к JSON-ключу)
# https://registry.terraform.io/providers/yandex-cloud/yandex/latest/docs#service_account_key_file
provider "yandex" {
  cloud_id  = var.yc_cloud_id
  folder_id = var.yc_folder_id
  zone      = var.yc_zone
}

data "yandex_compute_image" "ubuntu" {
  family = "ubuntu-2404-lts"
}

resource "yandex_compute_instance" "vm" {
  name        = "project-sem-1"
  platform_id = "standard-v3"
  zone        = var.yc_zone

  resources {
    cores  = 2
    memory = 2
  }

  boot_disk {
    initialize_params {
      image_id = data.yandex_compute_image.ubuntu.id
      size     = 20
    }
  }

  network_interface {
    subnet_id = yandex_vpc_subnet.subnet.id
    nat       = true
  }

  metadata = {
    ssh-keys = "${var.ssh_user}:${file(var.ssh_public_key_path)}"
  }
}

resource "yandex_vpc_network" "network" {
  name = "project-sem-1-network"
}

resource "yandex_vpc_subnet" "subnet" {
  name           = "project-sem-1-subnet"
  network_id     = yandex_vpc_network.network.id
  zone           = var.yc_zone
  v4_cidr_blocks = ["192.168.10.0/24"]
}

