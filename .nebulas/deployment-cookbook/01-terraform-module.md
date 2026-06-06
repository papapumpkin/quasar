+++
id = "terraform-module"
title = "Terraform module for the EC2 + IAM + SSM + EBS data volume; consumable from outside this repo"
type = "task"
priority = 2
depends_on = ["packer-ami"]
scope = [
    "deploy/terraform/**",
]
+++

## Problem

The AMI is reproducible; the deployment around it needs to be too. A single Terraform module wraps the EC2 + IAM + EBS + SSM wiring so a consumer in their own repo can drop in:

```hcl
module "quasar" {
  source  = "github.com/papapumpkin/quasar//deploy/terraform?ref=v0.2.0"
  version = "0.2.0"

  vpc_id          = "vpc-..."
  subnet_id       = "subnet-..."
  pat_ssm_arn     = aws_ssm_parameter.gh_pat.arn
  registered_repos = [
    { name = "papapumpkin/quasar",      git_url = "git@github.com:papapumpkin/quasar.git" },
    { name = "papapumpkin/relativity",  git_url = "git@github.com:papapumpkin/relativity.git" },
  ]
}
```

…and get a running Quasar.

## Solution

### Resources created

- **`aws_instance.quasar`** — t3.small, AMI ID from SSM `/quasar/latest-ami-id`, in the given subnet, root volume encrypted
- **`aws_ebs_volume.data`** — 100GB gp3, encrypted, attached as `/dev/sdf`; mounted to `/var/lib/quasar` via `user_data`
- **`aws_ebs_volume.repos`** — 200GB gp3, encrypted, attached as `/dev/sdg`; mounted to `/srv/repos`
- **`aws_iam_role.quasar`** + instance profile — assume-role policy for EC2
- **`aws_iam_role_policy.ssm_read`** — `ssm:GetParameter` scoped to `arn:aws:ssm:*:*:parameter/quasar/*`
- **`aws_iam_role_policy.cloudwatch_logs`** — `logs:CreateLogStream`, `PutLogEvents` for `/quasar/*` log groups
- **`aws_cloudwatch_log_group.quasar`** — 30-day retention
- **`aws_security_group.quasar`** — egress all; ingress 22 from the operator CIDR (input variable), 7330 (cockpit) from the operator CIDR
- **`aws_ssm_parameter.repos_json`** — `/quasar/repos.json` storing the registered repo list (read at boot by the user_data script)

### user_data

Runs once at boot. Three responsibilities:

1. Mount the data + repos EBS volumes (idempotent — checks if filesystem exists before mkfs)
2. Write `/etc/quasar/quasar.yaml` from `/quasar/quasar.yaml` SSM parameter (terraform-provided)
3. Read `/quasar/repos.json` from SSM and run `quasar repo register <path>` for each entry, cloning them first from the configured git URLs

```bash
#!/bin/bash
set -euo pipefail

# 1. Mount data volumes
/usr/local/sbin/quasar-mount-volumes.sh

# 2. Pull config from SSM
aws ssm get-parameter --name /quasar/quasar.yaml --with-decryption --query Parameter.Value --output text > /etc/quasar/quasar.yaml
chmod 0600 /etc/quasar/quasar.yaml
chown quasar:quasar /etc/quasar/quasar.yaml

# 3. Register repos
aws ssm get-parameter --name /quasar/repos.json --query Parameter.Value --output text > /tmp/repos.json
sudo -u quasar /usr/local/sbin/quasar-register-repos.sh /tmp/repos.json

# 4. Start the supervisor
systemctl start quasar
```

The `quasar-register-repos.sh` helper handles the GitHub-deploy-key setup (reading the SSH key from SSM and writing to `~quasar/.ssh/`), cloning each repo to `/srv/repos/<name>`, and calling `quasar repo register` for each.

### Variables

`deploy/terraform/variables.tf`:

```hcl
variable "vpc_id"                {}
variable "subnet_id"             {}
variable "pat_ssm_arn"           {}                                       # bot user PAT for gh
variable "deploy_key_ssm_arn"    {}                                       # SSH private key for repo clones
variable "registered_repos"      { type = list(object({ name = string, git_url = string })) }
variable "operator_cidr"         { default = "0.0.0.0/0" }                # narrow this!
variable "instance_type"         { default = "t3.small" }
variable "cockpit_enabled"       { default = false }
variable "data_volume_gb"        { default = 100 }
variable "repos_volume_gb"       { default = 200 }
variable "ami_id"                { default = null }                       # null = look up from SSM
variable "tags"                  { default = {} }
```

The default `operator_cidr = 0.0.0.0/0` is loud — the module's README warns to narrow it. A future enhancement is to require it explicitly (no default), but for v1 a documented default is fine.

### Outputs

```hcl
output "instance_id"           { value = aws_instance.quasar.id }
output "private_ip"            { value = aws_instance.quasar.private_ip }
output "iam_role_arn"          { value = aws_iam_role.quasar.arn }
output "cloudwatch_log_group"  { value = aws_cloudwatch_log_group.quasar.name }
output "cockpit_url"           { value = var.cockpit_enabled ? "http://${aws_instance.quasar.private_ip}:7330" : null }
```

### Tests

- `terraform validate` runs in CI
- `terraform plan` against a stub backend (`backend "local"` + `provider.aws.skip_credentials_validation = true`) — verifies the plan produces the expected resource count
- `tflint` and `tfsec` checks gate the module
- A lightweight integration test in `deploy/terraform/test/` using Terratest applies the module against a real AWS sandbox account on PRs labeled `terratest:run`

### Module versioning

The module is referenced via git tag: `?ref=v0.2.0`. The CI release workflow (existing) is updated to create a git tag on the module's directory contents matching the binary release tag, so the two stay aligned.

## Files

- `deploy/terraform/main.tf` (new)
- `deploy/terraform/variables.tf` (new)
- `deploy/terraform/outputs.tf` (new)
- `deploy/terraform/iam.tf` (new)
- `deploy/terraform/network.tf` (new) — security group
- `deploy/terraform/ssm.tf` (new)
- `deploy/terraform/user_data.sh.tftpl` (new)
- `deploy/terraform/scripts/quasar-mount-volumes.sh` (new)
- `deploy/terraform/scripts/quasar-register-repos.sh` (new)
- `deploy/terraform/README.md` (new) — usage example, variable reference
- `deploy/terraform/test/integration_test.go` (new, Terratest)
- `deploy/terraform/.tflint.hcl` (new)
- `.github/workflows/terraform-checks.yml` (new) — fmt + validate + tflint + tfsec on PRs

## Acceptance Criteria

- [ ] `terraform init && terraform validate` succeeds in `deploy/terraform/`
- [ ] `terraform plan` with a sample variable file produces a plan with the expected resources (EC2, two EBS volumes, IAM role + policies, log group, SG, SSM params)
- [ ] `tflint` and `tfsec` pass with no high-severity findings
- [ ] `user_data` mounts both EBS volumes idempotently; re-running on reboot does not reformat
- [ ] `quasar-register-repos.sh` clones repos in `repos.json` and calls `quasar repo register` for each
- [ ] The IAM role grants only `ssm:GetParameter` on `/quasar/*` and `logs:*` on its own log group
- [ ] Module README documents every variable and shows a complete usage example
- [ ] `terraform-checks.yml` workflow runs on PRs touching `deploy/terraform/**`
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
