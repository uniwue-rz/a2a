package main

// This application is a direct implementation of the
// http://docs.ansible.com/ansible/latest/dev_guide/developing_inventory.html
// It supports all the parameters and options detailed there.

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"encoding/json"
	"regexp"

	"github.com/urfave/cli"
	"gopkg.in/gcfg.v1"
	"gopkg.in/yaml.v2"
	"github.com/uniwue-rz/phabricator-go"
)

// AnsiblePlaybook will only be read in the Vagrant mode.
type AnsiblePlaybook struct {
	Host string `yaml:"hosts"`
}

// Configuration is managed using this struct
type Configuration struct {
	Phabricator struct {
		ApiToken string
		ApiURL   string
	}
	Ansible struct {
		Playbook string
	}
	Wrapper struct {
		Passphrase string
		Json       string
	}
}

// Output is used to encode the data for the output of the application
type Output struct {
	Group map[string]Group
	Meta  struct {
		HostVars map[string]map[string]interface{} `json:"hostvars, omitifempty"`
	} `json:"_meta"`
}

//MarshalJSON the json marshal for the output
func (output *Output) Sanitize() (data map[string]interface{}) {
	data = make(map[string]interface{})
	for k, v := range output.Group {
		data[k] = v
	}
	data["_meta"] = output.Meta

	return data
}

// Group is the data contains
type Group struct {
	Hosts []string               `json:"hosts, omitifempty"`
	Vars  map[string]interface{} `json:"vars, omitifempty"`
}

// AddHost adds a new host to the given host group in the output
func (output *Output) AddHost(host string, groupName string) {
	for k, v := range output.Group {
		if k == groupName {
			v.Hosts = append(v.Hosts, host)
			output.Group[k] = v
		}
	}
}

// Augment adds the result from passphrase and sanitizes the json results, so everything looks polished.
func (output *Output) Augment(request *phabricator.Request, PassphraseWrapper string, JsonWrapper string) {
	for k,v := range output.Meta.HostVars{
		for i, j := range v {
			passphrase, isPassphrase, _ := HandlePassphrase(request, PassphraseWrapper, j.(string))
			if isPassphrase {
				v[i] = passphrase
			}
			jsonData, isJson, _ := HandleJson(JsonWrapper, j.(string))
			if isJson {
				v[i] = jsonData
			}
		}
		output.Meta.HostVars[k] = v
	}
	for k, v := range output.Group {
		for i, j := range v.Vars {
			passphrase, isPassphrase, _ := HandlePassphrase(request, PassphraseWrapper, j.(string))
			if isPassphrase {
				v.Vars[i] = passphrase
			}
			jsonData, isJson, _ := HandleJson(JsonWrapper, j.(string))
			if isJson {
				v.Vars[i] = jsonData
			}
		}
		output.Group[k] = v
	}
}

// HandleJson returns converts the json to representable strings
func HandleJson(JsonWrapper string, propertyKey string) (m interface{}, isJson bool, err error) {
	isJson = false
	jsonRegex := regexp.MustCompile(JsonWrapper)
	if jsonRegex.MatchString(propertyKey) {
		err = json.Unmarshal([]byte(propertyKey), &m)
		isJson = true
	}

	return m, isJson, err
}

// HandlePassphrase returns the passphrase for the given system
func HandlePassphrase(request *phabricator.Request, PassphraseWrapper string, propertyKey string) (passPhrase string, isPassphrase bool, err error) {
	isPassphrase = false
	passPhraseRegex := regexp.MustCompile(PassphraseWrapper)
	if passPhraseRegex.MatchString(propertyKey) {
		passPhraseRegexMatching := passPhraseRegex.FindStringSubmatch(propertyKey)
		if len(passPhraseRegexMatching) > 1 {
			isPassphrase = true
			passPhraseKey := passPhraseRegexMatching[1]
			passphraseObj, err := phabricator.GetPassPhrase(request, passPhraseKey)
			if err != nil {
				passPhrase = ""
			}
			for _, passphraseItem := range passphraseObj.Result.Data {
				if passphraseItem.Monogram == passPhraseKey {
					if passphraseItem.Material.Password != "" {
						passPhrase = passphraseItem.Material.Password
					}
					if passphraseItem.Material.PrivateKey != "" {
						passPhrase = passphraseItem.Material.PrivateKey
					}
				}
			}

		}
	}

	return passPhrase, isPassphrase, err
}

//List Returns the json list of hosts and their properties
func List(request *phabricator.Request, vagrant string, playBookPath string) (output Output, err error) {
	groupList := make(map[string]Group)
	services, err := phabricator.GetServices(request)
	hostVars := make(map[string]map[string]interface{})
	// Returns the List of services
	if err != nil {
		panic(err)
	}
	for _, v := range services.Result.Data {
		var group Group
		// Add the hosts from the binding
		for _, v := range v.Attachments.Bindings.Bindings {
			values, err := CreateHost(request, v.Interface.Device.Name)
			if err != nil {
				panic(err)
			}

			hostVars[v.Interface.Device.Name] = values
			group.Hosts = append(group.Hosts, v.Interface.Device.Name)
		}
		vars := make(map[string]interface{})
		for _, v := range v.Attachments.Properties.Properties {
			key := ReplaceToUnderscore(v.Key)
			vars[key] = v.Value
		}
		group.Vars = vars
		// This fixes the problem with the groups with empty hosts.
		if len(group.Hosts) == 0 {
			group.Hosts = []string{}
		}
		groupList[v.Fields.Name] = group
	}
	output.Meta.HostVars = hostVars
	output.Group = groupList
	// If the list is running in vagrant mode
	if vagrant != "" {
		playbook, err := ReadAnsiblePlayBook(playBookPath)
		if err != nil {
			panic(err)
		}
		output.AddHost(vagrant, playbook[0].Host)
	}

	return output, err
}

// ReplaceToUnderscore simply replaces the dashes in the given text to underscore for the given keys.
func ReplaceToUnderscore(key string) string{
	re := regexp.MustCompile("-")

	return re.ReplaceAllString(key, "_")
}

// CreateHost Creates the host for the given device name
func CreateHost(request *phabricator.Request, devName string) (values map[string]interface{}, err error) {
	values = make(map[string]interface{})
	device, err := phabricator.GetDevice(request, devName)
	if err != nil {
		panic(err)
	}
	// Collect the properties
	for _, v := range device.Result.Data {
		for _, i := range v.Attachments.Properties.Properties {
			key := ReplaceToUnderscore(i.Key)
			values[key] = i.Value
		}
	}

	return values, err
}

// ReadAnsiblePlayBook reads the given playbook from path and decode it to AnsiblePlaybook.
func ReadAnsiblePlayBook(path string) (playbook []AnsiblePlaybook, err error) {
	buffer, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(buffer, &playbook)

	return playbook, err
}

// GetConfigPaths returns the list of config paths for the given system
func GetConfigPaths() []string {
	usr, _ := user.Current()
	homePath := usr.HomeDir + "/.a2a/config"

	return []string{"/etc/a2a/config", homePath, "config"}
}

// ReadConfig reads the configuration from the configurations
func ReadConfig() (Config Configuration, err error) {
	found := false
	for _, v := range GetConfigPaths() {
		if found == false {
			err := gcfg.ReadFileInto(&Config, v)
			if err == nil {
				found = true
			}
		}
	}
	if found == false {
		return Config, errors.New("the configuration is not found in one of /etc/a2a/config, ~/.a2a/config or config or there was an error reading the config file")
	}
	return Config, err
}

// CreateCommandLine creates a command line for the application
func CreateCommandLine() *cli.App {
	app := cli.NewApp()
	app.Version = "0.0.1"
	app.Author = "Pouyan Azari"
	app.EnableBashCompletion = true
	app.Name = "A2A"
	app.Usage = "Almanac2Ansible helps you to use your Almanac inventory as Ansible dynamic inventory"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "vagrant, a",
			Usage: "Vagrant mode which needs the name of the host to be added to the given service",
		},
		cli.BoolFlag{
			Name:  "list, l",
			Usage: "Lists the Services and Hosts in a way readable by Ansible.",
		},
		cli.StringFlag{
			Name:  "host, s",
			Usage: "List the properties for the given host",
		},
	}

	return app
}

// AugmentHost augments the given host with th
func AugmentHost(request * phabricator.Request, hostData map[string]interface{}, PassphraseWrapper string, JsonWrapper string) map[string]interface{}{
 for k,v := range hostData {
	 passphrase, isPassphrase, _ := HandlePassphrase(request, PassphraseWrapper, v.(string))
	 if isPassphrase {
		 hostData[k] = passphrase
	 }
	 jsonData, isJson, _ := HandleJson(JsonWrapper, v.(string))
	 if isJson {
		 hostData[k] = jsonData
	 }
	}

	return hostData
}

// Main Application
func main() {
	// Read the configuration
	Config, err := ReadConfig()
	if err != nil {
		panic(err)
	}
	request := phabricator.NewRequest(Config.Phabricator.ApiURL, Config.Phabricator.ApiToken)
	app := CreateCommandLine()
	app.Action = func(c *cli.Context) error {
		// Check if the vagrant mode is on
		vagrant := c.String("vagrant")
		listIsOn := c.Bool("list")
		// Manage the --list command
		if listIsOn {
			list, err := List(&request, vagrant, Config.Ansible.Playbook)
			if err != nil {
				panic(err)
			}
			list.Augment(&request, Config.Wrapper.Passphrase, Config.Wrapper.Json)
			printedData := list.Sanitize()
			jsonData, err := json.Marshal(printedData)
			fmt.Print(string(jsonData))
		}
		// Manage the --host command
		host := c.String("host")
		if host != ""{
			hostData, _ := CreateHost(&request, host)
			hostData = AugmentHost(&request, hostData, Config.Wrapper.Passphrase, Config.Wrapper.Json)
			jsonData, _ := json.Marshal(hostData)

			fmt.Print(string(jsonData))
		}
		return nil
	}
	app.Run(os.Args)
}
