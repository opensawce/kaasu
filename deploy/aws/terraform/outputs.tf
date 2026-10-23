output "bucket" {
  value       = module.shop.bucket
  description = "Source of truth for the shop's encrypted data; restored on task start."
}

output "log_group" {
  value       = module.shop.log_group
  description = "CloudWatch log group for the shop's task."
}
