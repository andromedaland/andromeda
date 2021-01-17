// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
data "aws_vpc" "default" {
  tags = {
    "aws:cloudformation:logical-id" = "VPC"
  }
}

data "aws_subnet_ids" "default_vpc" {
  vpc_id = data.aws_vpc.default.id
}

data "aws_subnet" "default_vpc" {
  for_each = data.aws_subnet_ids.default_vpc.ids
  id       = each.value
}

data "aws_security_group" "default" {
  vpc_id = data.aws_vpc.default.id

  filter {
    name   = "group-name"
    values = ["default"]
  }
}