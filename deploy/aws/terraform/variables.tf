variable "shop_id" {
  type        = string
  description = "Lower-case id, 3 to 32 characters of a-z, 0-9 and hyphen."
  validation {
    condition     = can(regex("^[a-z0-9-]{3,32}$", var.shop_id))
    error_message = "shop_id must be 3 to 32 lower-case letters, digits or hyphens."
  }
}

variable "kms_alias" {
  type        = string
  description = "Alias of the customer-managed KMS key that encrypts the shop's bucket."
  default     = "alias/kaasu-shops"
}
