variable "oidc_provider_arn" {
  description = "ARN of the EKS cluster IAM OIDC provider."
  type        = string
  validation {
    condition     = can(regex("^arn:[^:]+:iam::[0-9]{12}:oidc-provider/", var.oidc_provider_arn))
    error_message = "oidc_provider_arn must identify an IAM OIDC provider."
  }
}

variable "oidc_issuer_url" {
  description = "HTTPS issuer URL reported by the EKS cluster."
  type        = string
  validation {
    condition     = startswith(var.oidc_issuer_url, "https://")
    error_message = "oidc_issuer_url must use HTTPS."
  }
}

variable "kubernetes_namespace" {
  type    = string
  default = "kube-system"
}

variable "kubernetes_service_account" {
  type    = string
  default = "aws-load-balancer-controller"
}

variable "role_name" {
  type = string
}

variable "policy_name" {
  type = string
}
