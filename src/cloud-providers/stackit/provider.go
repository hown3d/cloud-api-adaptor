package stackit

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"net/url"
	"strconv"

	provider "github.com/confidential-containers/cloud-api-adaptor/src/cloud-providers"
	"github.com/confidential-containers/cloud-api-adaptor/src/cloud-providers/util"
	"github.com/confidential-containers/cloud-api-adaptor/src/cloud-providers/util/cloudinit"
	"github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas/wait"
)

const maxInstanceNameLen = 63

var (
	logger            = log.New(log.Writer(), "[adaptor/cloud/stackit] ", log.LstdFlags|log.Lmsgprefix)
	errFlavorNotFound = errors.New("flavor not found")
)

var _ provider.Provider = (*stackitProvider)(nil)

type stackitProvider struct {
	config *Config
	client *iaas.APIClient
}

func newProvider(c *Config) (*stackitProvider, error) {
	opts := []config.ConfigurationOption{
		config.WithRegion(c.Region),
	}
	if c.IaasURL != "" {
		u, err := url.Parse(c.IaasURL)
		if err != nil {
			return nil, err
		}
		opts = append(opts, config.WithEndpoint(u.String()))
	}
	client, err := iaas.NewAPIClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating stackit iaas client: %w", err)
	}
	return &stackitProvider{
		config: c,
		client: client,
	}, nil
}

// ConfigVerifier implements provider.Provider.
func (s *stackitProvider) ConfigVerifier() error {
	return nil
}

// CreateInstance implements provider.Provider.
func (s *stackitProvider) CreateInstance(ctx context.Context, podName string, sandboxID string, cloudConfig cloudinit.CloudConfigGenerator, spec provider.InstanceTypeSpec) (instance *provider.Instance, err error) {
	instanceName := util.GenerateInstanceName(podName, sandboxID, maxInstanceNameLen)

	machineType, err := s.flavorForSpec(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("getting machine type for spec %+v: %w", spec, err)
	}

	logger.Printf("starting instance %s with machine type %s", instanceName, machineType)

	srcImage := s.config.Image
	if spec.Image != "" {
		logger.Printf("Choosing %s from annotation as the STACKIT image for the PodVM image", spec.Image)
		srcImage = spec.Image
	}

	userData, err := cloudConfig.Generate()
	if err != nil {
		return nil, err
	}

	userDataRaw := []byte(userData)
	securityGroups := []string{s.config.SecurityGroupID}

	payload := iaas.CreateServerPayload{
		Name: &instanceName,
		// ImageId:     &srcImage,
		MachineType: &machineType,
		UserData:    &userDataRaw,
		Networking: &iaas.CreateServerPayloadNetworking{
			CreateServerNetworking: &iaas.CreateServerNetworking{
				NetworkId: &s.config.Network,
			},
		},
		AvailabilityZone: &s.config.AvailabilityZone,
		SecurityGroups:   &securityGroups,
		BootVolume: &iaas.CreateServerPayloadBootVolume{
			Size: iaas.PtrInt64(int64(s.config.RootVolumeSize)),
			Source: &iaas.BootVolumeSource{
				Type: iaas.PtrString("image"),
				Id:   &srcImage,
			},
		},
	}

	if s.config.KeyPairName != "" {
		payload.KeypairName = &s.config.KeyPairName
	}

	server, err := s.client.CreateServer(ctx, s.config.ProjectID).CreateServerPayload(payload).Execute()
	if err != nil {
		return nil, fmt.Errorf("creating server: %w", err)
	}

	_, err = wait.CreateServerWaitHandler(ctx, s.client, s.config.ProjectID, server.GetId()).WaitWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("waiting for server to get ready: %w", err)
	}
	logger.Printf("machine %s is ready", instanceName)

	server, err = s.client.GetServer(ctx, s.config.ProjectID, server.GetId()).Details(true).Execute()
	if err != nil {
		return nil, fmt.Errorf("getting server details: %w", err)
	}

	ips, err := getIPs(server)
	if err != nil {
		return nil, fmt.Errorf("get IPs from server: %w", err)
	}

	return &provider.Instance{
		Name: server.GetName(),
		ID:   server.GetId(),
		IPs:  ips,
	}, nil
}

// If vCPU and memory are set in annotations then use it
// If machine type is set in annotations then use it (ie. shape <system_type>-<cpu>x<memoery>)
// vCPU and Memory gets higher priority than instance type from annotation
func (s *stackitProvider) flavorForSpec(ctx context.Context, spec provider.InstanceTypeSpec) (string, error) {
	resp, err := s.client.ListMachineTypes(ctx, s.config.ProjectID).Execute()
	if err != nil {
		return "", err
	}
	flavors := resp.GetItems()

	specs := []provider.InstanceTypeSpec{}
	allInstances := make([]string, 0, len(flavors))

	for _, f := range flavors {
		c, ok := f.GetExtraSpecs()["overcommit"]
		if !ok {
			continue
		}
		overcommitStr, ok := c.(string)
		if !ok {
			continue
		}
		overcommit, err := strconv.ParseInt(overcommitStr, 10, 32)
		if err != nil {
			logger.Printf("error parsing overcommit for flavor %s", f.GetName())
			continue
		}
		if !s.config.UseDedicatedInstances && overcommit == 1 {
			continue
		}

		specs = append(specs, provider.InstanceTypeSpec{
			InstanceType: f.GetName(),
			VCPUs:        f.GetVcpus(),
			Memory:       f.GetRam(),
			// Arch: "",
			// GPUs: 0,
		})
		allInstances = append(allInstances, f.GetName())
	}

	return provider.SelectInstanceTypeToUse(spec, provider.SortInstanceTypesOnResources(specs), allInstances, s.config.MachineType)
}

func getIPs(s *iaas.Server) ([]netip.Addr, error) {
	var ips []netip.Addr
	for _, nic := range s.GetNics() {
		if s, ok := nic.GetIpv4Ok(); ok {
			ipv4, err := netip.ParseAddr(s)
			if err != nil {
				return nil, err
			}
			ips = append(ips, ipv4)
		}
		if s, ok := nic.GetIpv6Ok(); ok {
			ipv6, err := netip.ParseAddr(s)
			if err != nil {
				return nil, err
			}
			ips = append(ips, ipv6)
		}
	}
	return ips, nil
}

// DeleteInstance implements provider.Provider.
func (s *stackitProvider) DeleteInstance(ctx context.Context, instanceID string) error {
	// TODO: cleanup volume
	err := s.client.DeleteServerExecute(ctx, s.config.ProjectID, instanceID)
	if err != nil {
		return fmt.Errorf("deleting server: %w", err)
	}
	return nil
}

// Teardown implements provider.Provider.
func (s *stackitProvider) Teardown() error {
	return nil
}
