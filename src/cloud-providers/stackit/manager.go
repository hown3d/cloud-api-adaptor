package stackit

import (
	"flag"

	provider "github.com/confidential-containers/cloud-api-adaptor/src/cloud-providers"
)

var stackitcfg Config

type Config struct {
	ProjectID             string
	Region                string
	Image                 string
	Network               string
	IaasURL               string
	MachineType           string
	SecurityGroupID       string
	UseDedicatedInstances bool
	AvailabilityZone      string
	KeyPairName           string
	RootVolumeSize        int
}

type Manager struct{}

func init() {
	provider.AddCloudProvider("stackit", &Manager{})
}

func (_ *Manager) ParseCmd(flags *flag.FlagSet) {
	flags.StringVar(&stackitcfg.ProjectID, "stackit-project-id", "", "STACKIT project ID")
	flags.StringVar(&stackitcfg.Region, "stackit-region", "eu01", "STACKIT region")
	flags.StringVar(&stackitcfg.Image, "stackit-image", "", "Pod VM image id")
	flags.StringVar(&stackitcfg.Network, "stackit-network-id", "", "STACKIT network to put the podvm into")
	flags.StringVar(&stackitcfg.IaasURL, "stackit-iaas-endpoint-url", "", "STACKIT iaas endpoint")
	flags.StringVar(&stackitcfg.MachineType, "stackit-machine-type", "c2i.1", "machine type to use as a default for pod vms")
	flags.StringVar(&stackitcfg.AvailabilityZone, "stackit-availability-zone", "", "AvailabilityZone to deploy the pod VMs into")
	flags.StringVar(&stackitcfg.SecurityGroupID, "stackit-security-group", "", "SecurityGroupID to add to the pod VMs")
	flags.StringVar(&stackitcfg.KeyPairName, "stackit-key-pair", "", "SSH Keypair name to use for access to the pod VM")
	flags.IntVar(&stackitcfg.RootVolumeSize, "stackit-root-volume-size", 20, "Size of root volume for pod vm")
	flags.BoolVar(&stackitcfg.UseDedicatedInstances, "stackit-dediciated-instances", false, "Wether to use dedicated instances without overcommit on STACKIT IaaS")
}

// LoadEnv will be called by peerpod ctrl to load config
func (_ *Manager) LoadEnv() {
	provider.DefaultToEnv(&stackitcfg.ProjectID, "STACKIT_PROJECT_ID", "")
	provider.DefaultToEnv(&stackitcfg.IaasURL, "STACKIT_IAAS_URL", "")
	provider.DefaultToEnv(&stackitcfg.Region, "STACKIT_REGION", "")
}

func (_ *Manager) NewProvider() (provider.Provider, error) {
	return newProvider(&stackitcfg)
}

func (_ *Manager) GetConfig() (config *Config) {
	return &stackitcfg
}
