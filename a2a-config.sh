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