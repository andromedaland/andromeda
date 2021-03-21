// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
output "ledger_table" {
  value = aws_dynamodb_table.ledger.name
}

output "modules_queue_name" {
  value = aws_sqs_queue.modules.name
}

output "modules_queue_url" {
  value = aws_sqs_queue.modules.id
}