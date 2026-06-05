+++
id = "packer-ami"
title = "Packer build for a reproducible Quasar AMI on Ubuntu 24.04 LTS with all CLIs and the binary baked in"
type = "task"
priority = 2
scope = [
    "deploy/packer/**",
    ".github/workflows/build-ami.yml",
]
+++

## Problem

The supervisor needs `git`, `gh`, `claude`, and the `quasar` binary all present at boot. Installing them via cloud-init on first boot wastes minutes and creates a window where the instance is up but Quasar isn't. A baked AMI fixes both: boot → systemd starts → supervisor is reading state within seconds.

The AMI build must be reproducible. Same source → same AMI (modulo the inevitable timestamp in the AMI metadata). This means pinned package versions, pinned binary downloads with SHA-256 verification, and no `apt update && apt upgrade` at build time (we set the package versions explicitly).

## Solution

### Packer template

`deploy/packer/quasar.pkr.hcl`:

```hcl
packer {
  required_plugins {
    amazon = { source = "github.com/hashicorp/amazon", version = "~> 1.3" }
  }
}

variable "region"            { default = "us-east-1" }
variable "source_ami_id"     {}                            # pinned Ubuntu 24.04 minimal LTS
variable "quasar_version"    {}                            # git SHA or version tag
variable "gh_version"        { default = "2.55.0" }
variable "claude_version"    { default = "1.0.0" }
variable "ami_name_prefix"   { default = "quasar" }

source "amazon-ebs" "quasar" {
  region          = var.region
  source_ami      = var.source_ami_id
  instance_type   = "t3.small"
  ssh_username    = "ubuntu"
  ami_name        = "${var.ami_name_prefix}-${var.quasar_version}-{{timestamp}}"
  ami_description = "Quasar supervisor — ${var.quasar_version}"

  tags = {
    quasar_version = var.quasar_version
    builder        = "packer"
  }

  launch_block_device_mappings {
    device_name = "/dev/sda1"
    volume_size = 20
    volume_type = "gp3"
    encrypted   = true
    delete_on_termination = true
  }
}

build {
  sources = ["source.amazon-ebs.quasar"]

  provisioner "file" {
    source      = "files/"
    destination = "/tmp/quasar-build/"
  }

  provisioner "shell" {
    environment_vars = [
      "QUASAR_VERSION=${var.quasar_version}",
      "GH_VERSION=${var.gh_version}",
      "CLAUDE_VERSION=${var.claude_version}",
    ]
    scripts = [
      "scripts/01-base.sh",
      "scripts/02-install-clis.sh",
      "scripts/03-install-quasar.sh",
      "scripts/04-systemd.sh",
      "scripts/05-harden.sh",
    ]
  }
}
```

### Provisioner scripts

- `01-base.sh` — `apt-get install -y --no-install-recommends git=<pinned> sqlite3 ca-certificates curl`; disable unattended-upgrades
- `02-install-clis.sh` — curl gh and claude binaries from official URLs with SHA-256 verification; install to `/usr/local/bin`; verify `gh --version` and `claude --version`
- `03-install-quasar.sh` — copy the quasar binary from `files/quasar` (CI puts it there before invoking packer); install to `/usr/local/bin/quasar`; verify `quasar version`
- `04-systemd.sh` — install `files/quasar.service` to `/etc/systemd/system/`; `systemctl enable quasar` (does not start — instance is being baked)
- `05-harden.sh` — disable root SSH login, set `unattended-upgrades` to security-only, install logrotate config for `/var/log/quasar/*`, ensure `/var/lib/quasar` directory exists with correct ownership

### CI workflow

`.github/workflows/build-ami.yml`:

```yaml
name: build-ami
on:
  push:
    tags: ["v*"]
  workflow_dispatch:
    inputs:
      quasar_version: { required: true, type: string }

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      id-token: write       # for AWS OIDC
      contents: read
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: 'go.mod' }
      - run: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags cockpit -o deploy/packer/files/quasar .
      - uses: hashicorp/setup-packer@v3
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ secrets.AMI_BUILD_ROLE_ARN }}
          aws-region: us-east-1
      - run: cd deploy/packer && packer init . && packer build -var quasar_version=${GITHUB_REF_NAME:-${{ inputs.quasar_version }}} -var source_ami_id=$(./scripts/latest-ubuntu-2404-ami.sh) quasar.pkr.hcl
      - id: ami
        run: echo "ami_id=$(jq -r '.builds[0].artifact_id' manifest.json | cut -d: -f2)" >> $GITHUB_OUTPUT
        working-directory: deploy/packer
      - name: write AMI ID to SSM
        run: aws ssm put-parameter --name /quasar/latest-ami-id --value ${{ steps.ami.outputs.ami_id }} --type String --overwrite
```

The latest AMI ID is written to SSM so the Terraform module (Phase 1) can read it without GitHub Releases parsing.

### Reproducibility checks

- Build the same git SHA twice; compare the two AMIs' installed package versions and binary hashes (no diff expected).
- Test in `scripts/test-image.sh`: launch an instance from the AMI, SSH in, run `quasar version` and `quasar doctor`, assert both exit 0.

### What's *not* in the AMI

- No repo source code (those live on `/srv/repos/` mounted from EBS data volumes in Phase 1)
- No bot user PAT (lives in SSM, fetched at supervisor start)
- No `.quasar.yaml` (lives at `/etc/quasar/quasar.yaml`, written by Terraform user_data)

## Files

- `deploy/packer/quasar.pkr.hcl` (new)
- `deploy/packer/scripts/01-base.sh` (new)
- `deploy/packer/scripts/02-install-clis.sh` (new)
- `deploy/packer/scripts/03-install-quasar.sh` (new)
- `deploy/packer/scripts/04-systemd.sh` (new)
- `deploy/packer/scripts/05-harden.sh` (new)
- `deploy/packer/scripts/latest-ubuntu-2404-ami.sh` (new)
- `deploy/packer/scripts/test-image.sh` (new)
- `deploy/packer/files/quasar.service` (new) — base unit, refined in Phase 2
- `deploy/packer/files/quasar.yaml.example` (new)
- `.github/workflows/build-ami.yml` (new)

## Acceptance Criteria

- [ ] `packer build deploy/packer/quasar.pkr.hcl` produces an AMI when given a quasar binary in `files/`
- [ ] All binary downloads verify SHA-256 against pinned values
- [ ] `test-image.sh` launches an instance, SSHes in, and verifies `quasar version`, `quasar doctor`, `gh --version`, `claude --version` all exit 0
- [ ] Building the same git SHA twice produces AMIs with identical installed package lists (verified by a diff script)
- [ ] CI workflow `build-ami` runs on tag push, publishes AMI ID to SSM `/quasar/latest-ami-id`
- [ ] No PAT or `.quasar.yaml` baked into the AMI
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
