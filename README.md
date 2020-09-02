你好！
很冒昧用这样的方式来和你沟通，如有打扰请忽略我的提交哈。我是光年实验室（gnlab.com）的HR，在招Golang开发工程师，我们是一个技术型团队，技术氛围非常好。全职和兼职都可以，不过最好是全职，工作地点杭州。
我们公司是做流量增长的，Golang负责开发SAAS平台的应用，我们做的很多应用是全新的，工作非常有挑战也很有意思，是国内很多大厂的顾问。
如果有兴趣的话加我微信：13515810775  ，也可以访问 https://gnlab.com/，联系客服转发给HR。
# docker-plugin-cinder

This Docker volume plugin for utilizing OpenStack Cinder for persistent storage volumes.

The plugin attaches block storage volumes to the compute instance running the plugin. If the volume is already attached to another compute instance it will be detached first.


## Requirements

* Block Storage API v3
* Compute API v2
* KVM w/ virtio


## Setup

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

## Usage

The default volume size is 10GB but can be overridden:

```
$ docker volume create -d cinder -o size=20 volname
```


## Notes

### Machine ID

This plugins expects `/etc/machine-id` to be the OpenStack compute instance UUID which seems to be the case when booting cloud images with KVM. Otherwise configure `machineID` in the configuration file.

### Attaching volumes

Requested volumes that are already attached will be forcefully detached and moved to the requesting machine.


## License

MIT License
