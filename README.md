# A2A

Almanac2Ansible is simple Go Application that creates a dynamic inventory
compatible with Ansible from the Almanac inventory in Phabricator. All the options mentioned
in [Ansible Website](http://docs.ansible.com/ansible/latest/dev_guide/developing_inventory.html) are
supported. Only exception is the child service which is not possible in Almanac.

## Requirements

You need a working Phabricator instance with API access to Almanac enabled.

## Download

The binaries can be [downloaded](https://github.com/uniwue-rz/a2a/releases) from github website. For every release there
will be binaries for popular architectures.

## Installation

To Install this application you should copy the binary file to an
executable folder like `/usr/local/bin` and the application can
be called using the `a2a` in commandline.

## Configuration

This software accepts these three places as configuration file location
in the given order:

- `/etc/a2a/config`
- `~/.a2a/config`
- `config`

The configuration file should contain the following data:

```lang=config
[Phabricator]
ApiURL = URL of the Phabricator API
ApiToken = Token for Phabricator API

[Ansible]
Playbook = The Path to Ansible Playbook

[Wrapper]
Passphrase = "^\\((?P<name>[a-z-A-Z0-9.-]+)\\)$" # passphrase should always be in ()
Json = "^(\\[.*\\]|\\{.*\\})" # This is how the applications finds the data is a json data
```

## Usage

This software works in combination with Almanac inventory data.

### Almanac Data

This is a step by step guide to add data in Almanac so it can be used
by A2A dynamic inventory:

* Add a Network in Almanac, with the IP range you want.
* Add your host as a Device in Almanac, with the IP address or hostname as
name.
* In Device settings add a new interface with IP address of the device
and port 22 within the Network you created before.
* For Host Groups of Ansible  you can use Service in Almanac. Go to Service and add a new Service with type
Custom Service. The name is the group name you want ex. mysql-servers.
* Set the visibility and the project the service belongs to.
* Assign the host to group or in Almanac terms bind the Device to the given Service. This can be
done in given Service, Service Bindings tab.
* Map the interface you created in device here.
* The variables in Almanac are the properties that can be added to both Service
and Device. These will be translated to Group and Host variables.

A2A support both host variables and group variables. [Ansible variable precedence](http://docs.ansible.com/ansible/latest/playbooks_variables.html#variable-precedence-where-should-i-put-a-variable) takes host variables over group variables. The variables
should follow a special syntax. You should take care in your playbook that YAML files can not
have `-` (dashes) in variable name. Almanac does not support underscore `_`  in
property keys. Therefore when you add a new property, the key is added as a dashed value.
A2A automatically convert the dashes to underscore in the dynamic inventory result.
In your playbook you can use the variable with underscores `_`.  Your variable values can be
anything if you add JSON text it will be parsed as JSON, everything else will be parsed as string.
The same also applies to the group names, they should not contain `-` or `.`.

Example:
Almanac property:

```lang=conf
key: database-user
value: root
```
A2A inventory generated data:

```lang=json
{
"database_user" : "root"
}
```
In Playbook you can use it as:

```lang=yaml
{{database_user}}
```

As properties are plainly visible in Almanac, They are a poor choice for secret
variables or passwords. This problem is mitigated in A2A by using the Phabricator Passphrase conduit
API. To add a secure variable you should follow these steps:

* You should first add it as private key (ex. SSL, SSH keys) or password
in Passphrase. You will get a monogram that starts with `K` like `K42`. Don't forget
to allow API access to the passphrase by clicking the Allow Conduit Access button.
* In Service or Host add a new property with key that you want and as value add
the monogram in parenthesis. So `K42` would be `(K42)`.
* A2A will automatically translate this to the given Monogram to passphrase data.
`
### Dynamic Inventory
To use the dynamic inventory you should point the A2A with the -i option
so simply run the playbook with:
`
```lang=bash
ansible-playbook -i /usr/local/bin/a2a
```

### Vagrant Mode

Dynamic inventory can also be used in Vagrant with the help of Vagrant mode. This is for the
scenario of having local Ansible running the playbook on the machine. The mode
requires an specific `Vagrantfile` and the location of playbook.yml which is normally in `/vagrant/playbook.yml`.
In this method you add your vagrant machine to the given host group in Ansible playbook. For that you should tweak the A2A command
to run in vagrant mode with the help of simple shell script. Create a file `/usr/local/bin/a2a-vagrant` on your box machine.

```lang=bash
#!/bin/bash
/usr/local/bin/a2a --vagrant $VAGRANT_MACHINE "$@"
```

Then have a Vagrantfile as follows:
```lang=ruby
# -*- mode: ruby -*-
# vi: set ft=ruby :
# Start the configuration

Vagrant.configure("2") do |config|
  N = 1
  VAGRANT_VM_PROVIDER = "virtualbox"
  PHABRICATOR_API_URL = ""
  PHABRICATOR_API_TOKEN = ""
  ANSIBLE_PLAYBOOK_PATH = "/vagrant/playbook.yml"
  A2A_PASSPHRASE_WRAPPER  = "^\\\\\\((?P<name>[a-z-A-Z0-9.-]+)\\\\\\)$"
  A2A_JSON_WRAPPER = "^(\\\\\\[.*\\\\\\]|\\\\\\{.*\\\\\\})"
  config.vm.box = "your-dist"
  config.vm.box_check_update = true
  # Provision the Machines
  (1..N).each do |machine_id|
    config.vm.define "machine#{machine_id}" do |machine|
      machine.vm.hostname = "machine#{machine_id}"
      machine.vm.network "private_network", ip: "192.168.77.#{20+machine_id}"
      machine.vm.provider "virtualbox" do |vb|
        vb.memory = "2048"
      end
      machine.vm.provision :shell, :path =>"a2a.sh", :args => [PHABRICATOR_API_TOKEN, PHABRICATOR_API_URL, ANSIBLE_PLAYBOOK_PATH, A2A_PASSPHRASE_WRAPPER, A2A_JSON_WRAPPER]
      machine.vm.provision :shell, :path =>"script.sh", :args => [machine#{machine_id}]
      machine.vm.provision :ansible_local do |ansible|
          ansible.playbook = "playbook.yml"
          ansible.verbose = true
          ansible.limit = "machine#{machine_id}"
          ansible.inventory_path = "/usr/local/bin/a2a-vagrant"
          ansible.raw_arguments = [
             "--private-key=/vagrant/.vagrant/machines/machine#{machine_id}/#{VAGRANT_VM_PROVIDER}/private_key"
          ]
      end
    end
  end
end
```

The script that creates the config file for a2a can be the following

```lang=bash
#!/usr/bin/env bash
# Creates the a2a configuration, removes the existing one
[ -f /etc/a2a/config ]  && rm -f /etc/a2a/config
[ -f $HOME/.a2a/config ] && rm -f $HOME/.a2a/config
[ -d /etc/a2a ] && rm -rf /etc/a2a
[ -d $HOME/.a2a ] && rm -rf $HOME/.a2a

mkdir /etc/a2a

echo "[Phabricator]" >> /etc/a2a/config
echo "ApiToken = \"$1\"" >> /etc/a2a/config
echo "ApiURL = \"$2\"" >> /etc/a2a/config
echo "[Ansible]" >> /etc/a2a/config
echo "Playbook = \"$3\"" >> /etc/a2a/config
echo "[Wrapper]" >> /etc/a2a/config
echo "Passphrase = \"$4\"" >>  /etc/a2a/config
echo "Json = \"$5\"" >> /etc/a2a/config
```

As it is visible you need a script.sh to the magic, this script just set your `$VAGRANT_MACHINE` variable. You can use this
as example:

```lang=bash
#!/usr/bin/env bash
#Replace .profile with .bashrc if required
source /etc/profile.local
if [ -z "$VAGRANT_MACHINE" ]; then
    echo "export VAGRANT_MACHINE=$1" >> /etc/profile.local
fi
if [ -z "$ANSIBLE_HOST_KEY_CHECKING" ]; then
    echo "export ANSIBLE_HOST_KEY_CHECKING=False" >> /etc/profile.local
fi
source /etc/profile.local
```

These files exists in repository as `script.sh.dist`, `a2a-config.sh.dist` and `Vagrantfile.dist`.

### No Cache Mode

The internal cache of the application can be disable using the 
`--no-cache` option.

## Prometheus Monitoring

This inventory system can also used almanac to create the dynamic monitoring for every devices (hosts) and
services (groups). The property `prometheus-config` will be used to create the scrape data for the groups
and devices existing in almanac. You can overwrite the group `prometheus-config` in a given host.

To run the inventory in prometheus mode you should call:

```lang=bash
a2a -p
```

The configuration should be a json value that should contain the following values:

```lang=json
{
    "name" : "job-name-in-prometheus",
    "port": "the-port-the-exporter-listens",
    "exporter": "the-name-of-the-exporter"
}
```

The group,host and ip address will be added by the inventory on hand your service, device configurations.
The result will be something like this:

```lang=json
[{"labels":{"group":"service","host":"device-name","ip":"device-address","job":"job-name"},"targets":["device-address:port"]}]
```

## Prometheus Alerting

Prometheus alerting consist of two parts, the first part is the rules that the alerts should be sent based on them. At the moment
this rules are managed in a central repository on the prometheus machine and only prometheus admin can change them. The second part
uses the AlertManager to dispatch the alert to the right place. A2A can be used to create `route` and `receivers` part of the alert manager
based on the services (groups) in phabricator.  With this it is possible to send errors to the given destination.

You can use this script to create your alerting mechanism for the given hosts. For that to a group (service) in almanac, you will need to
add the following configuration `alertmanager-config` as property:

```lang=json
[{"type":"email","name":"unique-name-of-receiver","config":{"to":"email-of-user"}}]
```

This will be converted to the given route:

```lang=yaml
routes:
    group: "name-of-the-group"
    receiver: unique-name-of-receiver
    
receivers:
    name: unique-name-of-receiver
    email_config:
        - to: email-of-user
```

To use this part of inventory, you should simply give the existing yaml 
configuration as input and this script will read that file and adds your changes
and print the file in output. You should write it copy it back in the right place.

```lang=bash
a2a -m /etc/alert-manager/alert-manager.yaml
```

## Prometheus Blackbox

Most of Prometheus metrics are added as whitebox like node exporter or mysql-exporter. These can be added to a given host or service
automatically with the help of Prometheus monitoring. For blackbox prometheus-blackbox is used. The configuration for the blackbox
module is consist of two parts. A module which is the module that is used in your blackbox configuration and the targets for the given
module. and example would be

```lang=json
[{"module":"http_2xx","targets":["https://uni-wuerzburg.de"]}]
```

It creates the following dynamic prometheus configuration:

```lang=json
[{"labels":{"group":"device-service","host":"device-host-name","ip":"device-ip","job":"blackbox","module":"http_2xx"},"targets":["https://uni-wuerzburg.de"]}]
```

The `module` is a parameter and it should be relabeled, the configuration would be:

```lang=yaml
 - job_name: blackbox
   metrics_path: /probe
   file_sd_configs:
   - refresh_interval: 2m
     files:
       - '/etc/prometheus/dynamic/*_blackbox.json'
   relabel_configs:
    - source_labels: [module]
      target_label: __param_module
    - source_labels: [__address__]
      target_label: __param_target
    - source_labels: [__param_target]
      target_label: instance
    - target_label: __address__
      replacement: 127.0.0.1:9115 # The blackbox exporter.
``` 

The blackbox mode can be called with help of `-b` and if some groups 
should be ignored these can be added as comma separated values with `-i`.

## Build

You can build your own binaries from the source code, using golang standard
procedure.

```lang=bash
git clone https://github.com/uniwue-rz/a2a.git
cd a2a
export GOPATH=´YOUR GO PATH´
go get
go build
```

## RoadMap

There are several points that should be covered in the next couple of
releases:
- Test case: Add test cases for the application
- Native Packages: This has low priority, but will replace the binary
releases at some point.

## License

See LICENCE file


