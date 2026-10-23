# Per-shop managed hosting in ap-south-1. Plain Terraform, no apply from this repository.
terraform {
  required_version = ">= 1.6"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}

provider "aws" {
  region = "ap-south-1"
  default_tags {
    tags = { project = "kaasu", managed_by = "terraform" }
  }
}

module "shop" {
  source    = "./modules/shop"
  shop_id   = var.shop_id
  kms_alias = var.kms_alias
}
