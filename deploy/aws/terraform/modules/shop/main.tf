variable "shop_id" { type = string }
variable "kms_alias" { type = string }

data "aws_kms_alias" "shops" {
  name = var.kms_alias
}

# The shop's source of truth: encrypted with the customer-managed key, versioned, no public access.
resource "aws_s3_bucket" "data" {
  bucket = "kaasu-${var.shop_id}-data"
}

resource "aws_s3_bucket_versioning" "data" {
  bucket = aws_s3_bucket.data.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "data" {
  bucket = aws_s3_bucket.data.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = data.aws_kms_alias.shops.target_key_arn
    }
  }
}

resource "aws_s3_bucket_public_access_block" "data" {
  bucket                  = aws_s3_bucket.data.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_cloudwatch_log_group" "task" {
  name              = "/kaasu/${var.shop_id}"
  retention_in_days = 90
}

output "bucket" { value = aws_s3_bucket.data.bucket }
output "log_group" { value = aws_cloudwatch_log_group.task.name }
