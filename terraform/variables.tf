// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
variable "env" {
  description = "short slug of the environment name"
  type        = string
}

variable "region" {
  description = "default aws region"
  type        = string
}

variable "ecs_instance_type" {
  description = "ECS worker nodes instance type"
  type        = string
}