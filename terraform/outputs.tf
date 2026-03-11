output "vm_id" {
  value       = yandex_compute_instance.vm.id
  description = "VM instance ID"
}

output "vm_ip" {
  value       = yandex_compute_instance.vm.network_interface[0].nat_ip_address
  description = "Public IP address of the VM"
}
