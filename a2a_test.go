package main

import (
	phabricator "app-a2a/api-phabricator-go"
	"encoding/json"
	"testing"
)

func TestReadConfig(t *testing.T) {
	config, err := ReadConfig()
	if err != nil {
		panic(err)
	}
	if config.Phabricator.ApiToken == "" {
		panic("ApiToken is empty")
	}
}

// Test List function, mainly for profiling it
//func TestList(t *testing.T) {
//	Config, err := ReadConfig()
//	if err != nil {
//		panic(err)
//	}
//	p := phabricator.NewPhabricator(Config.Phabricator.ApiURL, Config.Phabricator.ApiToken)
//	vagrant := ""
//	_, err = List(p, Config.Ansible.Playbook, vagrant)
//	if err != nil {
//		panic(err)
//	}
//}

func TestListWithAugmentParallel(t *testing.T)  {
	Config, err := ReadConfig()
	if err != nil {
		panic(err)
	}

	p := phabricator.NewPhabricator(Config.Phabricator.ApiURL, Config.Phabricator.ApiToken)
	vagrant := ""
	list, err := ListParallel(p, Config.Ansible.Playbook, vagrant)
	if err != nil {
		panic(err)
	}

	list.AugmentParallel(p, Config.Wrapper.Passphrase, Config.Wrapper.Json)
	printedData := list.Sanitize()
	_, err = json.Marshal(printedData)
}

func TestListWithAugmentBlocking(t *testing.T)  {
	Config, err := ReadConfig()
	if err != nil {
		panic(err)
	}

	p := phabricator.NewPhabricator(Config.Phabricator.ApiURL, Config.Phabricator.ApiToken)
	vagrant := ""
	list, err := ListBlocking(p, Config.Ansible.Playbook, vagrant)
	if err != nil {
		panic(err)
	}

	list.AugmentBlocking(p, Config.Wrapper.Passphrase, Config.Wrapper.Json)
	printedData := list.Sanitize()
	_, err = json.Marshal(printedData)
}