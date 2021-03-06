package main

// This application is a direct implementation of the
// http://docs.ansible.com/ansible/latest/dev_guide/developing_inventory.html
// It supports all the parameters and options detailed there.

import (
	"./alertmanager/config"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/uniwue-rz/phabricator-go"
	"github.com/urfave/cli"
	"gopkg.in/gcfg.v1"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cacheFile = "a2a_cache"

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

type BlackboxInput struct {
	Module  string   `json:"module, omitifempty"`
	Targets []string `json:"targets"`
}

// PrometheusOutput is used to create
// the output that can be read by prometheus
type PrometheusOutput struct {
	Labels  map[string]string `json:"labels, omitifempty"`
	Targets interface{}       `json:"targets, omitifempty"`
}

// readAlertManagerConfig Reads the alert manager config file and returns
// The configuration data and content as byte. If there is an error
// reading the file it panics
func readAlertManagerConfig(path string) (*config.Config, []byte, error) {
	dataConfig, content, err := config.LoadFile(path)
	if err != nil {
		panic(err)
	}

	return dataConfig, content, err
}

func manageAlertManager(p *phabricator.Phabricator, configPath string, jsonWrapper string) {
	dataConfig, _, err := readAlertManagerConfig(configPath)
	if err != nil {
		panic(err)
	}
	routes, receivers := getGroupRouteReceivers(p, jsonWrapper)
	dataConfig = addRouteReceivers(dataConfig, routes, receivers)
	fmt.Println(dataConfig)
}

func getGroupRouteReceivers(p *phabricator.Phabricator, jsonWrapper string) (routes []config.Route, receivers []config.Receiver) {
	services, err := p.GetServicesAsync()

	if err != nil {
		panic(err)
	}

	for _, d := range services.Result.Data {
		matchArray := make(map[string]string)
		groupName := d.Fields.Name
		matchArray["group"] = groupName
		for _, property := range d.Attachments.Properties.Properties {
			if property.Key == "alertmanager-config" {
				val, isJson, _ := HandleJson(jsonWrapper, property.Value)
				if isJson {
					for _, data := range val.([]interface{}) {
						name, nameOk := data.(map[string]interface{})["name"]
						alertType, alertTypeOk := data.(map[string]interface{})["type"]
						receiverConfig, receiverConfigOK := data.(map[string]interface{})["receiver-config"]
						matchInConfig, matchingConfigOk := data.(map[string]interface{})["matching-config"]
						// Adds the matching config to the match array if extra information exist
						if matchingConfigOk {
							for k, val := range matchInConfig.(map[string]string) {
								// The group can not be changed. It is a security
								// feature added so the groups always match Almanac
								if k != "group" {
									matchArray[k] = val
								}
							}
						}
						if nameOk && alertTypeOk && receiverConfigOK {
							// The is marked by the A2A so it can be found again
							receiverName := "dynamic-" + groupName + "-" + alertType.(string) + "-" + name.(string)
							route := config.Route{
								Receiver: receiverName,
								Match:    matchArray,
							}
							routes = append(routes, route)
							receiver := config.Receiver{Name: receiverName}
							if alertType == "email" {
								toEmail, toEmailOK := receiverConfig.(map[string]interface{})["to"]
								emailConfig := config.EmailConfig{}
								if toEmailOK {
									emailConfig.To = toEmail.(string)
								}
								textEmail, textEmailOk := receiverConfig.(map[string]interface{})["text"]
								if textEmailOk {
									emailConfig.Text = textEmail.(string)
								}
								requireTLS, requireTLSOk := receiverConfig.(map[string]interface{})["require-tls"]
								emailConfig.RequireTLS = new(bool)
								if requireTLSOk {
									if requireTLS.(string) == "false" {
										* emailConfig.RequireTLS = false
									} else {
										* emailConfig.RequireTLS = true
									}
								} else {
									* emailConfig.RequireTLS = false
								}
								sendResolved, sendResolvedOk := receiverConfig.(map[string]interface{})["send-resolved"]
								emailConfig.VSendResolved = true
								if sendResolvedOk {
									if sendResolved.(string) == "false" {
										emailConfig.VSendResolved = false
									}
								}
								receiver.EmailConfigs = append(receiver.EmailConfigs, &emailConfig)
							}
							receivers = append(receivers, receiver)
						}
					}
				}
			}
		}
	}
	return routes, receivers
}

// addRouteReceivers Adds the routes and receivers to the existing configuration.
func addRouteReceivers(alertManagerConfig *config.Config, routes []config.Route, receivers []config.Receiver) *config.Config {
	for _, route := range routes {
		routeToAdd := route
		handleExistingRoute(alertManagerConfig, route)
		alertManagerConfig.Route.Routes = append(alertManagerConfig.Route.Routes, &routeToAdd)
	}
	for _, receiver := range receivers {
		receiverToAdd := receiver
		handleExistingReceiver(alertManagerConfig, receiver)
		alertManagerConfig.Receivers = append(alertManagerConfig.Receivers, &receiverToAdd)
	}
	return alertManagerConfig
}

// handleExistingRoute Checks if the alert-manager configuration contains the given route
func handleExistingRoute(alertManagerConfig *config.Config, route config.Route) {
	k := 0
	for _, alertRoute := range alertManagerConfig.Route.Routes {
		if alertRoute.Receiver == route.Receiver && reflect.DeepEqual(route.Match, alertRoute.Match) {
			alertManagerConfig.Route.Routes = alertManagerConfig.Route.Routes[:k+copy(
				alertManagerConfig.Route.Routes[k:], alertManagerConfig.Route.Routes[k+1:])]
		}
		k++
	}
}

// handleExistingReceiver Checks if the alert manager configuration contains the given receiver
func handleExistingReceiver(alertManagerConfig *config.Config, receiver config.Receiver) {
	for k, alertReceiver := range alertManagerConfig.Receivers {
		if alertReceiver.Name == receiver.Name {
			alertManagerConfig.Receivers = alertManagerConfig.Receivers[:k+copy(
				alertManagerConfig.Receivers[k:], alertManagerConfig.Receivers[k+1:])]
		}
	}
}

//MarshalJSON the json marshal for the output
func (output *Output) Sanitize() (data map[string]interface{}) {
	data = make(map[string]interface{})
	for k, v := range output.Group {
		k = ReplaceToUnderscore(k)
		k = ReplaceDotsToUnderscore(k)
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

// Save the result json data in the cache.
// If the cache is not to old this will be returned as the result
func saveCache(jsonData []byte, path string) {
	cacheFileObject, err := getTempFilePath(path)
	if err != nil {
		panic(err)
	}
	thePath, err := filepath.Abs(cacheFileObject.Name())
	err = ioutil.WriteFile(thePath, jsonData, 0644)
	if err != nil {
		panic(err)
	}
}

// Reads the cache file and see if is still valid
// If still valid it will be used.
// The cacheAge should be given in minutes
func readCache(path string, cacheAge int) (jsonData []byte, cacheStatus bool, err error) {
	now := time.Now()
	then := now.Add(time.Duration(-cacheAge) * time.Minute)
	cacheFileObject, err := getTempFilePath(path)
	if err != nil {
		return jsonData, false, err
	}
	thePath, err := filepath.Abs(filepath.Dir(cacheFileObject.Name()))
	if err != nil {
		return jsonData, false, err
	}
	file, err := os.Stat(thePath)
	if err == nil {
		diff := then.Before(file.ModTime())
		if diff {
			jsonData, err = ioutil.ReadAll(cacheFileObject)
			if err != nil {
				return jsonData, false, err
			}
			if len(jsonData) == 0 {
				return jsonData, false, err
			}
			return jsonData, true, err
		}
		return jsonData, false, err
	} else if os.IsNotExist(err) {
		err = nil
		return jsonData, false, err
	} else {
		return jsonData, false, err
	}
}

// Augment adds the result from passphrase and sanitizes the json results, so everything looks polished.
func (output *Output) AugmentParallel(p *phabricator.Phabricator, PassphraseWrapper string, JsonWrapper string) {

	var wg sync.WaitGroup
	wg.Add(len(output.Meta.HostVars))

	var hostVarsSyncMap sync.Map
	for k, v := range output.Meta.HostVars {
		go func(k string, v map[string]interface{}) {
			defer wg.Done()
			for i, j := range v {
				passphrase, isPassphrase, _ := HandlePassphrase(p, PassphraseWrapper, j.(string))
				if isPassphrase {
					v[i] = passphrase
				}
				jsonData, isJson, _ := HandleJson(JsonWrapper, j.(string))
				if isJson {
					v[i] = jsonData
				}
			}

			hostVarsSyncMap.Store(k, v)

		}(k, v)
	}
	wg.Wait()
	hostVarsSyncMap.Range(func(k interface{}, v interface{}) bool {
		output.Meta.HostVars[k.(string)] = v.(map[string]interface{})
		return true
	})

	wg.Add(len(output.Group))
	var outputGroupSyncMap sync.Map
	for k, v := range output.Group {
		go func(k string, v Group) {
			defer wg.Done()
			for i, j := range v.Vars {
				passphrase, isPassphrase, _ := HandlePassphrase(p, PassphraseWrapper, j.(string))
				if isPassphrase {
					v.Vars[i] = passphrase
				}
				jsonData, isJson, _ := HandleJson(JsonWrapper, j.(string))
				if isJson {
					v.Vars[i] = jsonData
				}
			}
			outputGroupSyncMap.Store(k, v)
		}(k, v)
	}
	wg.Wait()
	outputGroupSyncMap.Range(func(k interface{}, v interface{}) bool {
		output.Group[k.(string)] = v.(Group)
		return true
	})
}

// Augment adds the result from passphrase and sanitizes the json results, so everything looks polished.
func (output *Output) AugmentBlocking(p *phabricator.Phabricator, PassphraseWrapper string, JsonWrapper string) {
	for k, v := range output.Meta.HostVars {
		for i, j := range v {
			passphrase, isPassphrase, _ := HandlePassphrase(p, PassphraseWrapper, j.(string))
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
			passphrase, isPassphrase, _ := HandlePassphrase(p, PassphraseWrapper, j.(string))
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

func (output *Output) Augment(p *phabricator.Phabricator, PassphraseWrapper string, JsonWrapper string) {
	output.AugmentParallel(p, PassphraseWrapper, JsonWrapper)
}

// GetBlackBoxData returns the blackbox targets and data.
func GetBlackBoxData(p *phabricator.Phabricator, JsonWrapper string, ignoreArray []string) (allOutputs []PrometheusOutput, err error) {
	services, err := p.GetServicesAsync()

	allOutputs = make([]PrometheusOutput, 0)
	if err != nil {
		return allOutputs, err
	}
	for _, d := range services.Result.Data {
		ignored := false
		for _, b := range ignoreArray {
			if b == d.Fields.Name && ignored == false {
				ignored = true
			}
		}
		if len(d.Attachments.Bindings.Bindings) != 0 && ignored == false {
			blackBoxConfig := ""
			for _, property := range d.Attachments.Properties.Properties {
				if property.Key == "blackbox-config" {
					blackBoxConfig = property.Value
				}
			}
			for _, v := range d.Attachments.Bindings.Bindings {
				host, err := CreateHost(p, v.Interface.Device.Name)

				if val, ok := host["blackbox_config"]; ok {
					blackBoxConfig = val.(string)
				}

				_, isJson, err := HandleJson(JsonWrapper, blackBoxConfig)
				if err != nil {
					panic(err)
				}

				if isJson {
					var blackBoxJson []BlackboxInput
					err := json.Unmarshal([]byte(blackBoxConfig), &blackBoxJson)
					if err != nil {
						panic(err)
					}
					for _, blackbox := range blackBoxJson {
						labels := make(map[string]string, 0)
						group := d.Fields.Name
						labels["module"] = blackbox.Module
						labels["job"] = "blackbox"
						labels["group"] = group
						labels["ip"] = v.Interface.Address
						labels["host"] = v.Interface.Device.Name
						targets := blackbox.Targets
						prometheusOutput := PrometheusOutput{
							Labels:  labels,
							Targets: targets}
						allOutputs = append(allOutputs, prometheusOutput)
					}

				}
			}

		}
	}
	return allOutputs, err
}

// GetPrometheusData returns the monitoring data for every host and group. If the host has its own
// prometheus-config this will be used, when not the group settings will be used.
// The script will be used here to create the dynamic configuration in Prometheus
func GetPrometheusData(p *phabricator.Phabricator, JsonWrapper string, ignoreArray []string) (allOutputs []PrometheusOutput, err error) {
	services, err := p.GetServicesAsync()

	allOutputs = make([]PrometheusOutput, 0)
	if err != nil {
		return allOutputs, err
	}
	for _, d := range services.Result.Data {
		ignored := false
		for _, b := range ignoreArray {
			if b == d.Fields.Name && ignored == false {
				ignored = true
			}
		}
		if len(d.Attachments.Bindings.Bindings) != 0 && ignored == false {
			prometheusConfig := ""
			groupPrometheusConfig := ""
			for _, property := range d.Attachments.Properties.Properties {
				if property.Key == "prometheus-config" {
					prometheusConfig = property.Value
					groupPrometheusConfig = property.Value
				}
			}
			for _, v := range d.Attachments.Bindings.Bindings {
				host, err := CreateHost(p, v.Interface.Device.Name)

				if val, ok := host["prometheus_config"]; ok {
					prometheusConfig = val.(string)
				}else{
					prometheusConfig = groupPrometheusConfig
				}

				m, isJson, err := HandleJson(JsonWrapper, prometheusConfig)
				if err != nil {
					panic(err)
				}
				if isJson {
					for _, data := range m.([]interface{}) {
						name, nameOk := data.(map[string]interface{})["name"]
						port, portOk := data.(map[string]interface{})["port"]
						if nameOk && portOk {
							targets := make([]string, 0)
							target := v.Interface.Address + ":" +
								strconv.FormatFloat(port.(float64), 'f', -1, 64)
							targets = append(targets, target)
							group := d.Fields.Name
							labels := make(map[string]string, 0)
							labels["job"] = name.(string)
							labels["group"] = group
							labels["ip"] = v.Interface.Address
							labels["host"] = v.Interface.Device.Name
							prometheusOutput := PrometheusOutput{
								Labels:  labels,
								Targets: targets}
							allOutputs = append(allOutputs, prometheusOutput)
						}
					}
				}
			}
		}
	}
	return allOutputs, err
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
func HandlePassphrase(p *phabricator.Phabricator, PassphraseWrapper string, propertyKey string) (passPhrase string, isPassphrase bool, err error) {
	isPassphrase = false
	passPhraseRegex := regexp.MustCompile(PassphraseWrapper)
	if passPhraseRegex.MatchString(propertyKey) {
		passPhraseRegexMatching := passPhraseRegex.FindStringSubmatch(propertyKey)
		if len(passPhraseRegexMatching) > 1 {
			isPassphrase = true
			passPhraseKey := passPhraseRegexMatching[1]
			passphraseObj, err := p.GetPassPhraseWithId(passPhraseKey)
			if err != nil {
				passPhrase = ""
				return passPhrase, false, err
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
func ListParallel(p *phabricator.Phabricator, playBookPath string, vagrant string) (output Output, err error) {

	groupList := make(map[string]Group)
	hostVars := make(map[string]map[string]interface{})

	services, err := p.GetServicesAsync() // -> one request, not worth paralleling

	// Returns the List of services
	if err != nil {
		panic(err)
	}

	type empty struct{}
	amountOfResultData := len(services.Result.Data)
	sem := make(chan empty, amountOfResultData) // semaphore pattern
	for _, d := range services.Result.Data {    // currently around 20 loops --> paralleling
		go func(d phabricator.Device) {
			var group Group
			// Add the hosts from the binding
			for _, v := range d.Attachments.Bindings.Bindings { // Anzahl Bindings: meistens zirka 1-2 --> erstmal nicht parallelisieren
				interfaceDeviceName := v.Interface.Device.Name
				values, _ := CreateHost(p, interfaceDeviceName) // -> one request
				hostVars[v.Interface.Device.Name] = values
				group.Hosts = append(group.Hosts, v.Interface.Device.Name)
			}

			vars := make(map[string]interface{})
			for _, v := range d.Attachments.Properties.Properties {
				key := ReplaceToUnderscore(v.Key)
				vars[key] = v.Value
			}
			group.Vars = vars

			// This fixes the problem with the groups with empty hosts.
			if len(group.Hosts) == 0 {
				group.Hosts = []string{}
			}
			groupList[d.Fields.Name] = group

			sem <- empty{}
		}(d)
	}

	// wait for goroutines to finish
	for i := 0; i < amountOfResultData; i++ {
		<-sem
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

func ListBlocking(p *phabricator.Phabricator, playBookPath string, vagrant string) (output Output, err error) {

	groupList := make(map[string]Group)
	hostVars := make(map[string]map[string]interface{})
	services, err := p.GetServicesAsync()

	// Returns the List of services
	if err != nil {
		panic(err)
	}
	for _, d := range services.Result.Data {
		var group Group
		// Add the hosts from the binding
		for _, v := range d.Attachments.Bindings.Bindings {
			interfaceDeviceName := v.Interface.Device.Name
			values, err := CreateHost(p, interfaceDeviceName)
			if err != nil {
				panic(err)
			}
			hostVars[v.Interface.Device.Name] = values
			group.Hosts = append(group.Hosts, v.Interface.Device.Name)
		}

		vars := make(map[string]interface{})
		for _, v := range d.Attachments.Properties.Properties {
			key := ReplaceToUnderscore(v.Key)
			vars[key] = v.Value
		}
		group.Vars = vars

		// This fixes the problem with the groups with empty hosts.
		if len(group.Hosts) == 0 {
			group.Hosts = []string{}
		}
		groupList[d.Fields.Name] = group
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

func List(p *phabricator.Phabricator, playBookPath string, vagrant string) (output Output, err error) {
	return ListParallel(p, playBookPath, vagrant)
}

// ReplaceToUnderscore simply replaces the dashes in the given text to underscore for the given keys.
func ReplaceToUnderscore(key string) string {
	re := regexp.MustCompile("-")
	return re.ReplaceAllString(key, "_")
}

// ReplaceDotsToUnderscore replaces the dots in the given text to underscore fot the given keys
func ReplaceDotsToUnderscore(key string) string {
	re := regexp.MustCompile("\\.")
	return re.ReplaceAllString(key, "_")
}

// CreateHost Creates the host for the given device name
func CreateHost(p *phabricator.Phabricator, devName string) (values map[string]interface{}, err error) {
	values = make(map[string]interface{})

	device, err := p.GetDeviceAsync(devName) // -> one request
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
	usr, err := user.Current()
	if err != nil {
		return []string{"/etc/a2a/config", "config"}
	}
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
	app.Version = "0.0.14"
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
		cli.StringFlag{
			Name: "alertmanager, m",
			Usage: "Returns the alert manager settings, " +
				"it reads the existing file and adds the needed data to the alerts",
		},
		cli.BoolFlag{
			Name:  "blackbox, b",
			Usage: "Returns the list of services that support the blackbox",
		},
		cli.BoolFlag{
			Name:  "prometheus, p",
			Usage: "Returns the list of services supported by Prometheus for the given host",
		},
		cli.StringFlag{
			Name:  "ignore, i",
			Usage: "Make the Prometheus or blackbox exporter, ignore the given group",
		},
		cli.BoolFlag{
			Name:  "no-cache, n",
			Usage: "Run the application in no cache mode",
		},
	}

	return app
}

// AugmentHost augments the given host with th
func AugmentHost(p *phabricator.Phabricator, hostData map[string]interface{}, PassphraseWrapper string, JsonWrapper string) map[string]interface{} {
	for k, v := range hostData {
		passphrase, isPassphrase, _ := HandlePassphrase(p, PassphraseWrapper, v.(string))
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

// Returns the path to the temporary file
// It uses the tmp path that used by the OS per default
func getTempFilePath(fileName string) (file *os.File, err error) {
	filePathGlob, err := filepath.Abs(os.TempDir() + fileName + "*")
	matches, err := filepath.Glob(filePathGlob)
	if err != nil {
		file, err = ioutil.TempFile(os.TempDir(), fileName)
		return file, err
	}
	if len(matches) == 1 {
		return os.Open(matches[0])
	}
	// Remove all the temp files that are not needed
	for _, path := range matches {
		err = os.Remove(path)
	}
	file, err = ioutil.TempFile(os.TempDir(), fileName)
	return file, err
}

// Main Application
func main() {
	// Read the configuration
	Config, err := ReadConfig()
	if err != nil {
		panic(err)
	}
	p := phabricator.NewPhabricator(Config.Phabricator.ApiURL, Config.Phabricator.ApiToken)
	app := CreateCommandLine()
	app.Action = func(c *cli.Context) error {
		// Check if the vagrant mode is on
		vagrant := c.String("vagrant")
		listIsOn := c.Bool("list")
		prometheusIsOn := c.Bool("prometheus")
		blackBoxIsOn := c.Bool("blackbox")
		cacheIsOff := c.Bool("no-cache")
		ignoreGroups := c.String("ignore")
		if vagrant != "" {
			cacheIsOff = true
		}
		// Manage the --list command
		if listIsOn {
			cachedData, cacheStatus, err := readCache(cacheFile, 10)
			if cacheStatus && !cacheIsOff {
				if err != nil {
					panic(err)
				}
				fmt.Print(string(cachedData))
				return nil
			}
			list, err := List(p, Config.Ansible.Playbook, vagrant)
			if err != nil {
				panic(err)
			}
			list.Augment(p, Config.Wrapper.Passphrase, Config.Wrapper.Json)
			printedData := list.Sanitize()
			jsonData, err := json.Marshal(printedData)
			saveCache(jsonData, cacheFile)
			fmt.Print(string(jsonData))
		}
		// Creates the blackbox settings with modules as labels.
		// Should use relabeling to make parameter from the label.
		if blackBoxIsOn {
			var ignoreArray []string
			if ignoreGroups != "" {
				ignoreArray = strings.Split(ignoreGroups, ",")
			}
			blackBoxData, err := GetBlackBoxData(p, Config.Wrapper.Json, ignoreArray)
			if err == nil {
				jsonData, _ := json.Marshal(blackBoxData)
				fmt.Println(string(jsonData))
			}
		}
		// Creates the prometheus dynamic scraps from the Almanac repo
		// --prometheus
		if prometheusIsOn {
			var ignoreArray []string
			if ignoreGroups != "" {
				ignoreArray = strings.Split(ignoreGroups, ",")
			}
			prometheusData, err := GetPrometheusData(p, Config.Wrapper.Json, ignoreArray)
			if err == nil {
				jsonData, _ := json.Marshal(prometheusData)
				fmt.Println(string(jsonData))
			}
		}
		// Manages the alertmanager command
		// Reads the alertManager configs and rewrites with new routes.
		// --alertmanager
		alertManagerConfigPath := c.String("alertmanager")
		if alertManagerConfigPath != "" {
			manageAlertManager(p, alertManagerConfigPath, Config.Wrapper.Json)
		}
		// Manage the --host command
		host := c.String("host")
		if host != "" {
			hostData, _ := CreateHost(p, host)
			hostData = AugmentHost(p, hostData, Config.Wrapper.Passphrase, Config.Wrapper.Json)
			jsonData, _ := json.Marshal(hostData)

			fmt.Print(string(jsonData))
		}
		return nil
	}
	err = app.Run(os.Args)
}
