// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
resource "aws_ecs_cluster" "this" {
  name = "${local.prefix}-cluster-${local.short_uuid}"
  capacity_providers = [
    "FARGATE",
    "FARGATE_SPOT",
    aws_ecs_capacity_provider.asg.name
  ]
  tags = local.tags

  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

resource "aws_ecs_capacity_provider" "asg" {
  name = "${local.prefix}-capacity-provider-${local.short_uuid}"

  auto_scaling_group_provider {
    auto_scaling_group_arn         = aws_autoscaling_group.ecs_asg.arn
    managed_termination_protection = "DISABLED"

    # managed_scaling {
    #   maximum_scaling_step_size = 1000
    #   minimum_scaling_step_size = 1
    #   status                    = "DISABLED"
    #   target_capacity           = 10
    # }
  }
}