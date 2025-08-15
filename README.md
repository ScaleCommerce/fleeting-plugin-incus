# fleeting-plugin-incus

## Example config
```yaml
  [runners.autoscaler]
    plugin = "fleeting-plugin-incus"
    capacity_per_instance = 1
    max_use_count = 2
    max_instances = 3
    [runners.autoscaler.plugin_config]
      incus_image = "runner-base"                    # Incus image/alias to use
      incus_instance_key_path = "/opt/ssh-fleeting/id_ed25519"  # SSH private key for VM access
      incus_instance_size = "c5-m10"                 # VM size: c5-m10 (5 CPU, 10GB) or t2.micro (AWS-style)
      incus_disk_size = "100GiB"                     # Root disk size (default: 100GiB)
      incus_startup_timeout = 120                    # VM startup timeout in seconds (default: 120)
      incus_operation_timeout = 60                   # Incus operation timeout in seconds (default: 60)
      incus_naming_scheme = "runner-$random"         # Naming scheme for VMs (default: runner-$random)
      max_instances = 5                              # Maximum number of VMs (default: 5)

    [runners.autoscaler.connector_config]
      username          = "root"
      use_external_addr = true

    [[runners.autoscaler.policy]]
      idle_count = 2
      idle_time = "20m0s"
```

### Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `incus_image` | `runner-base` | Incus image or alias to use for VMs |
| `incus_instance_key_path` | *required* | Path to SSH private key for VM access |
| `incus_instance_size` | `c1-m2` | VM size specification (CPU/RAM, see below) |
| `incus_disk_size` | `10GiB` | Root disk size for VMs (e.g., `50GiB`, `200GiB`) |
| `incus_startup_timeout` | `120` | Timeout in seconds for VM startup |
| `incus_operation_timeout` | `60` | Timeout in seconds for Incus operations |
| `incus_naming_scheme` | `runner-$random` | VM naming pattern |
| `max_instances` | `5` | Maximum number of concurrent VMs |

### VM Size Specifications

You can specify VM sizes in two formats:

1. **Incus format**: `c<CPU>-m<RAM_GB>` (e.g., `c2-m4` = 2 CPUs, 4GB RAM)
2. **AWS format**: `t2.micro`, `t3.small`, etc. (Incus maps these to equivalent specs)

### Disk Size Configuration

The `incus_disk_size` option controls the root disk size for VMs:

- **Default**: `100GiB` (recommended for GitLab CI/CD workloads)
- **Format**: Use standard size units (`GiB`, `GB`, `TiB`, etc.)
- **Examples**: `50GiB`, `200GiB`, `1TiB`
- **Minimum**: At least `10GiB` required for basic operations

**Note**: Disk size is set during VM creation and cannot be changed later without recreating the VM.

### ⚠️ Important: max_instances Configuration

**Make sure the `max_instances` values are consistent:**

```yaml
[runners.autoscaler]
  max_instances = 5                    # GitLab Runner limit

[runners.autoscaler.plugin_config]
  max_instances = 5                    # Plugin limit (must be >= runner limit)
```

**Error**: `max size option exceeds instance group's max size: X > Y`
- **Cause**: GitLab Runner's `max_instances` (X) > Plugin's `max_instances` (Y)  
- **Fix**: Set plugin's `max_instances` ≥ runner's `max_instances`
## Installation

### Using Make (Recommended)
```bash
git clone https://github.com/pkramme/fleeting-plugin-incus.git
cd fleeting-plugin-incus

# Build with version information
make build

# Install to system
make install
```

### Manual Build
```bash
git clone https://github.com/pkramme/fleeting-plugin-incus.git
cd fleeting-plugin-incus

# Simple build
go build -o fleeting-plugin-incus ./cmd/fleeting-plugin-incus

# Or build with version information
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0")
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_INFO=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")

go build -ldflags "-X 'fleeting-plugin-incus.BuildInfo=$BUILD_INFO' -X 'fleeting-plugin-incus.BuildDate=$BUILD_DATE' -X 'fleeting-plugin-incus.GitCommit=$GIT_COMMIT'" -o fleeting-plugin-incus ./cmd/fleeting-plugin-incus

# Install
sudo cp fleeting-plugin-incus /usr/local/bin/
```

### Check Version
```bash
fleeting-plugin-incus --version
```

You also need an image with docker installed and the public key of the key with path `incus_instance_key_path` deployed in the root user. This is then used by the gitlab-runner to run docker commands over SSH. The following should get you started:
```bash
# generate SSH keys
ssh-keygen -t ed25519

# create a base VM
incus launch images:ubuntu/22.04 runner-base --vm
incus exec runner-base bash

# inside the VM
apt-get update && apt-get install openssh-server docker.io nano -y

# deploy your generated ssh public key inside VM
mkdir ~/.ssh/
nano ~/.ssh/authorized_keys

# shutdown
poweroff

# create a base image from that VM
incus publish runner-base --alias runner-base --reuse

# and restart gitlab-runner. After that, VMs should be created.
systemctl restart gitlab-runner
```

## Troubleshooting

### Common Issues

#### 1. `max size option exceeds instance group's max size`
```bash
# Check current configuration
fleeting-plugin-incus --version

# View logs  
sudo journalctl -u gitlab-runner -f

# Fix: Adjust max_instances in GitLab Runner config
```

#### 2. `SSH key file does not exist`
```bash
# Check if SSH key exists
ls -la /opt/ssh-fleeting/id_ed25519

# Generate SSH key if missing
sudo mkdir -p /opt/ssh-fleeting
sudo ssh-keygen -t ed25519 -f /opt/ssh-fleeting/id_ed25519 -N ""
```

#### 3. `failed to create VM with image 'runner-base'`
```bash
# List available images
incus image list

# Create base image if missing (see installation section above)
```

#### 4. VMs created but jobs fail
```bash
# Test SSH connection to VM
ssh -i /opt/ssh-fleeting/id_ed25519 root@<VM_IP>

# Check if docker is running in VM
incus exec <VM_NAME> -- docker ps
```

## Development

### Logging Style Guide

The plugin uses a consistent logging format:

#### **Message Structure:**
- **Main Actions**: `Sentence case` (e.g. `Plugin ready`, `Scale up request`)
- **Sub Actions**: `Emoji [STAGE] Action` (e.g. `🔨 [CREATE] Creating VM`, `🗑️ [DELETE] Stopping VM`)
- **Status Messages**: Consistent terminology (`completed`, `failed`, `ready`)
- **Spacing**: Exactly **one space** between emoji and `[STAGE]`

#### **Lifecycle Stages:**
- `[INIT]` - Plugin initialization
- `[ANALYSIS]` - State analysis and planning
- `[CLEANUP]` - Cleanup operations
- `[CREATE]` - VM creation process
- `[DELETE]` - VM deletion process
- `[CONNECT]` - Connection info and SSH setup

#### **Emojis:**
- 🚀 Plugin lifecycle
- 📈📉 Scaling operations  
- 🔨 VM creation
- 🗑️ VM deletion
- ✅ Success
- ❌ Errors
- ⚠️ Warnings
- 🔍 Information gathering

#### **Field Names:**
- `vm_name` (for individual VMs)
- `vms_to_create` / `vms_to_delete` (for counts with clear intent)
- `vms_to_check` / `vms_to_cleanup` (for other operations)
- `error` (for error messages)
- `progress` (for operation progress)
