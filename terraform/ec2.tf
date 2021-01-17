// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
data "aws_ami" "bottlerocket_x86" {
  most_recent = true
  owners      = ["092701018921"]

  filter {
    name   = "name"
    values = ["bottlerocket-aws-ecs-1-x86_64-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

data "aws_iam_policy" "ecs_instance" {
  arn = "arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role"
}

data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "ecs_instance" {
  name               = "${local.prefix}-ecs-instance-role-${local.short_uuid}"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy_attachment" "ecs_instance" {
  role       = aws_iam_role.ecs_instance.name
  policy_arn = data.aws_iam_policy.ecs_instance.arn
}

resource "aws_iam_instance_profile" "ecs_instance" {
  name = "${local.prefix}-instance-profile-${local.short_uuid}"
  role = aws_iam_role.ecs_instance.name
}

resource "aws_launch_template" "ecs_worker" {
  name                    = "${local.prefix}-template-${local.short_uuid}"
  description             = "Launch Template for ECS cluter worker node"
  disable_api_termination = false
  image_id                = data.aws_ami.bottlerocket_x86.image_id
  instance_type           = var.ecs_instance_type
  vpc_security_group_ids  = [data.aws_security_group.default.id]

  block_device_mappings {
    device_name = "/dev/xvdb"

    ebs {
      delete_on_termination = "true"
      encrypted             = "false"
      volume_size           = 30
      volume_type           = "gp2"
    }
  }

  iam_instance_profile {
    arn = aws_iam_instance_profile.ecs_instance.arn
  }

  tags = local.tags
}

resource "aws_autoscaling_group" "ecs_asg" {
  name                      = "${local.prefix}-asg-${local.short_uuid}"
  min_size                  = 1
  max_size                  = 1
  vpc_zone_identifier       = data.aws_subnet_ids.default_vpc.ids
  capacity_rebalance        = false
  default_cooldown          = 300
  health_check_grace_period = 300
  health_check_type         = "EC2"
  metrics_granularity       = "1Minute"
  protect_from_scale_in     = true
  service_linked_role_arn   = "arn:aws:iam::831183038069:role/aws-service-role/autoscaling.amazonaws.com/AWSServiceRoleForAutoScaling"
  enabled_metrics = [
    "GroupDesiredCapacity",
    "GroupInServiceCapacity",
    "GroupInServiceInstances",
    "GroupMaxSize",
    "GroupMinSize",
    "GroupPendingCapacity",
    "GroupPendingInstances",
    "GroupStandbyCapacity",
    "GroupStandbyInstances",
    "GroupTerminatingCapacity",
    "GroupTerminatingInstances",
    "GroupTotalCapacity",
    "GroupTotalInstances",
  ]

  launch_template {
    name    = aws_launch_template.ecs_worker.name
    version = "$Latest"
  }
}