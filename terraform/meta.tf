// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
terraform {
  required_version = ">= 0.14"
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
  backend "s3" {
    profile = "andromeda-global"
    key     = "staging/terraform.tfstate"
    region  = "us-east-1"
  }
}

provider "aws" {
  region = var.region
}

resource "random_uuid" "this" {}
data "aws_caller_identity" "this" {}

locals {
  prefix     = "andromeda"
  short_uuid = substr(random_uuid.this.result, 0, 8)
  tags = {
    "deno.land/x:environment"    = var.env
    "deno.land/x:uuid"           = local.short_uuid
    "deno.land/x:provisioned-by" = reverse(split(":", data.aws_caller_identity.this.arn))[0]
  }
}