resource "aws_dynamodb_table" "ledger" {
  name         = "${local.prefix}-ledger-${local.short_uuid}"
  hash_key     = "specifier"
  billing_mode = "PAY_PER_REQUEST"

  attribute {
    name = "specifier"
    type = "S"
  }

  tags = local.tags
}

resource "aws_sqs_queue" "modules" {
  name                              = "${local.prefix}-modules-queue-${local.short_uuid}"
  content_based_deduplication       = false
  delay_seconds                     = 0
  fifo_queue                        = false
  kms_data_key_reuse_period_seconds = 300
  max_message_size                  = 262144
  message_retention_seconds         = 345600
  policy = jsonencode(
    {
      Id = "__default_policy_ID"
      Statement = [
        {
          Action = "SQS:*"
          Effect = "Allow"
          Principal = {
            AWS = "arn:aws:iam::${data.aws_caller_identity.this.account_id}:root"
          }
          Resource = "arn:aws:sqs:us-east-1:${data.aws_caller_identity.this.account_id}:andromeda-test-1"
          Sid      = "__owner_statement"
        },
      ]
      Version = "2008-10-17"
    }
  )
  receive_wait_time_seconds  = 0
  tags                       = local.tags
  visibility_timeout_seconds = 0
}