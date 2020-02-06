package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	_log "log"
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
)

const (
	appName = "docker-volume-plugin-cinder"
)

type tConfig struct {
	Debug                       bool
	Quiet                       bool
	Prefix                      string `json:"prefix,omitempty"`
	IdentityEndpoint            string `json:"endpoint,omitempty"`
	Username                    string `json:"username,omitempty"`
	Password                    string `json:"password,omitempty"`
	DomainID                    string `json:"domainID,omitempty"`
	DomainName                  string `json:"domainName,omitempty"`
	TenantID                    string `json:"tenantId,omitempty"`
	TenantName                  string `json:"tenantName,omitempty"`
	ApplicationCredentialID     string `json:"applicationCredentialId,omitempty"`
	ApplicationCredentialName   string `json:"applicationCredentialName,omitempty"`
	ApplicationCredentialSecret string `json:"applicationCredentialSecret,omitempty"`
	Region                      string `json:"region,omitempty"`
}

func init() {
	_log.SetOutput(ioutil.Discard)

	log.SetFormatter(&log.TextFormatter{DisableTimestamp: true})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)
}

func main() {
	var config tConfig
	var configFile string
	flag.BoolVar(&config.Debug, "debug", false, "Enable debug logging")
	flag.BoolVar(&config.Quiet, "quiet", false, "Only report errors")
	flag.StringVar(&configFile, "config", "", "")
	flag.StringVar(&config.Prefix, "prefix", "docker-volume", "")
	flag.Parse()

	if len(configFile) == 0 {
		configFile = "cinder.json"
	}

	log.SetFormatter(&log.TextFormatter{DisableTimestamp: true})
	log.SetOutput(os.Stdout)

	content, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatal(err.Error())
	}

	err = json.Unmarshal(content, &config)
	if err != nil {
		log.Fatal(err.Error())
	}

	if config.Quiet {
		log.SetLevel(log.ErrorLevel)
	}

	if config.Debug {
		log.SetLevel(log.DebugLevel)
	}

	if len(config.IdentityEndpoint) == 0 {
		log.Fatal("Identity endpoint missing")
	}

	opts := gophercloud.AuthOptions{
		IdentityEndpoint:            config.IdentityEndpoint,
		Username:                    config.Username,
		Password:                    config.Password,
		DomainID:                    config.DomainID,
		DomainName:                  config.DomainName,
		TenantID:                    config.TenantID,
		TenantName:                  config.TenantName,
		ApplicationCredentialID:     config.ApplicationCredentialID,
		ApplicationCredentialName:   config.ApplicationCredentialName,
		ApplicationCredentialSecret: config.ApplicationCredentialSecret,
		AllowReauth:                 true,
	}

	logger := log.WithField("endpoint", opts.IdentityEndpoint)
	logger.Info("Connecting...")

	provider, err := openstack.AuthenticatedClient(opts)
	if err != nil {
		logger.WithError(err).Fatal(err.Error())
	}

	endpointOpts := gophercloud.EndpointOpts{
		Region: config.Region,
	}

	plugin, err := newPlugin(provider, endpointOpts, &config)

	if err != nil {
		logger.WithError(err).Fatal(err.Error())
	}

	handler := volume.NewHandler(plugin)

	logger.Info("Connected.")

	err = handler.ServeUnix("cinder", 0)
	if err != nil {
		logger.WithError(err).Fatal(err.Error())
	}
}
