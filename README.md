# fleeting-plugin-incus

## Example config
```yaml
  [runners.autoscaler]
    plugin = "fleeting-plugin-incus"
    capacity_per_instance = 1
    max_use_count = 1
    max_instances = 2
    [runners.autoscaler.plugin_config]
      incus_image = "baseimage"
      incus_instance_key_path = "/root/.ssh/id_ed25519"

    [runners.autoscaler.connector_config]
      username          = "root"
      use_external_addr = true

    [[runners.autoscaler.policy]]
      idle_count = 1
      idle_time = "20m0s"
```
## Installation
```
git clone https://github.com/pkramme/fleeting-plugin-incus.git
cd fleeting-plugin-incus/cmd/fleeting-plugin-incus
go build
cp fleeting-plugin-incus /usr/bin/fleeting-plugin-incus
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
