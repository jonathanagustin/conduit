terraform {
  required_version = ">= 1.6.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.60.0"
    }
  }
}

locals {
  oidc_hostpath = trimprefix(var.oidc_issuer_url, "https://")
}

data "aws_iam_policy_document" "controller_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [var.oidc_provider_arn]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.oidc_hostpath}:aud"
      values   = ["sts.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.oidc_hostpath}:sub"
      values   = ["system:serviceaccount:${var.kubernetes_namespace}:${var.kubernetes_service_account}"]
    }
  }
}

resource "aws_iam_policy" "controller" {
  name        = var.policy_name
  description = "Upstream AWS Load Balancer Controller v3.4.3 policy"
  policy      = file("${path.module}/iam-policy.json")
}

resource "aws_iam_role" "controller" {
  name                 = var.role_name
  assume_role_policy   = data.aws_iam_policy_document.controller_assume_role.json
  max_session_duration = 3600
}

resource "aws_iam_role_policy_attachment" "controller" {
  role       = aws_iam_role.controller.name
  policy_arn = aws_iam_policy.controller.arn
}
