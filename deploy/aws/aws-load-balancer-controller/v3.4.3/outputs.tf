output "role_arn" {
  value = aws_iam_role.controller.arn
}

output "policy_arn" {
  value = aws_iam_policy.controller.arn
}
