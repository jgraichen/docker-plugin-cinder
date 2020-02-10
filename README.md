# docker-plugin-cinder

This Docker volume plugin for utilizing OpenStack Cinder for persistent storage volumes.

The plugin attaches block storage volumes to the compute instance running the plugin. If the volume is already attached to another compute instance it will be detached first.


## Requirements

* Block Storage API v3
* Compute API v2
* KVM w/ virtio


## Usage

Provide configuration for the plugin:

```
{
    "endpoint": "http://keystone.example.org/v3",
    "username": "username",
    "password": "password",
    "domainID: "",
    "domainName": "default"
    "tenantID": "",
    "tenantName": "",
    "applicationCredentialId": "",
    "applicationCredentialName": "",
    "applicationCredentialSecret": "",
    "region": "",
    "mountDir": ""
}
```

Run the daemon before docker:

```
$ /usr/local/bin/docker-plugin-cinder -config /path/to/config.json
INFO Connecting...                                 endpoint="http://api.os.xopic.de:5000/v3"
INFO Machine ID detected                           id=e0f89b1b-ceeb-4ec5-b8f1-1b9c274f8e7b
INFO Connected.                                    endpoint="http://api.os.xopic.de:5000/v3"
```

By default a `cinder.json` from the current working directory will be used.


## Notes

### Machine ID

This plugins expects `/etc/machine-id` to be the OpenStack compute instance UUID which seems to be the case when booting cloud images with KVM. Otherwise configure `machineID` in the configuration file.

### Attaching volumes

Requested volumes that are already attached will be forcefully detached and moved to the requesting machine.


## License

MIT License
