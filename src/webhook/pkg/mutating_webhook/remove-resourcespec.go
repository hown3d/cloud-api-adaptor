package mutating_webhook

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/confidential-containers/cloud-api-adaptor/src/webhook/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RUNTIME_CLASS_NAME_DEFAULT       = "kata-remote"
	POD_VM_EXTENDED_RESOURCE_DEFAULT = "kata.peerpods.io/vm"
	PEERPODS_CPU_ANNOTATION          = "io.katacontainers.config.hypervisor.default_vcpus"
	PEERPODS_MEMORY_ANNOTATION       = "io.katacontainers.config.hypervisor.default_memory"
	GPU_RESOURCE_NAME                = "nvidia.com/gpu"
	PEERPODS_GPU_ANNOTATION          = "io.katacontainers.config.hypervisor.default_gpus"
)

var logger = log.New(log.Writer(), "[pod-mutator] ", log.LstdFlags|log.Lmsgprefix)

// mutate POD spec
// remove the POD resource spec
func (a *PodMutator) mutatePod(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	var runtimeClassName string
	mpod := pod.DeepCopy()

	if runtimeClassName = os.Getenv("TARGET_RUNTIMECLASS"); runtimeClassName == "" {
		runtimeClassName = RUNTIME_CLASS_NAME_DEFAULT
	}
	// Mutate only if the POD is using specific runtimeClass
	if mpod.Spec.RuntimeClassName == nil || *mpod.Spec.RuntimeClassName != runtimeClassName {
		return mpod, nil
	}

	overhead, err := a.runtimeClassOverhead(ctx, runtimeClassName)
	if err != nil {
		return nil, fmt.Errorf("calculating runtimeclass overhead: %w", err)
	}

	mpod = adjustResourceSpec(mpod, overhead)

	return mpod, nil
}

func (a *PodMutator) runtimeClassOverhead(ctx context.Context, runtimeClassName string) (corev1.ResourceList, error) {
	rtc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: runtimeClassName,
		},
	}
	if err := a.Client.Get(ctx, client.ObjectKeyFromObject(rtc), rtc); err != nil {
		return corev1.ResourceList{}, fmt.Errorf("getting runtimeClass %s: %w", runtimeClassName, err)
	}
	if rtc.Overhead == nil {
		return corev1.ResourceList{}, nil
	}
	return rtc.Overhead.PodFixed, nil
}

// function to remove resource spec from the pod spec
// add the cumulative resources as annotation to pod spec
// add the peer-pod resource to the first container in the pod spec

func adjustResourceSpec(pod *corev1.Pod, overhead corev1.ResourceList) *corev1.Pod {
	// Get total CPU resource requests
	cpuRequest := utils.GetResourceRequestQuantity(pod, corev1.ResourceCPU)

	// Get total CPU resource limits
	cpuLimit := utils.GetResourceLimitQuantity(pod, corev1.ResourceCPU)

	// Get total Memory resource requests
	memoryRequest := utils.GetResourceRequestQuantity(pod, corev1.ResourceMemory)

	// Get total Memory resource limits
	memoryLimit := utils.GetResourceLimitQuantity(pod, corev1.ResourceMemory)

	// Get total GPU resource requests
	// GPU resources are always requested in whole numbers and request and limits are same.
	// So we don't need to check for limits
	gpuRequest := utils.GetResourceRequestQuantity(pod, corev1.ResourceName(GPU_RESOURCE_NAME))

	// log the resource values
	logger.Printf("CPU Request: %s, CPU Limit: %s, Memory Request: %s, Memory Limit: %s, GPU Request: %s",
		cpuRequest.String(), cpuLimit.String(), memoryRequest.String(), memoryLimit.String(), gpuRequest.String())

	// Add the cumulative resources as annotation to pod spec
	// Use cpuLimit and memoryLimit if those are greater than cpuResource and memoryResource
	annotations := pod.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// A non-existent request or limit will be 0 and we don't need to add annotation for 0
	// We only add annotation if the value is greater than 0
	// Limit will always be preferred over request

	// Add cpu annotation
	if !cpuRequest.IsZero() && cpuLimit.Cmp(cpuRequest) >= 0 {
		if overhead.Cpu() != nil {
			cpuLimit.Add(*overhead.Cpu())
		}
		logger.Printf("Adding CPU annotation based on cpuLimit (integer value): %d", cpuLimit.Value())
		// We need the scaled value for the annotation and not the raw value like 1000m, 0.4 etc for CPU
		annotations[PEERPODS_CPU_ANNOTATION] = strconv.FormatInt(cpuLimit.Value(), 10)
	} else if cpuRequest.Sign() == 1 {
		if overhead.Cpu() != nil {
			cpuRequest.Add(*overhead.Cpu())
		}
		logger.Printf("Adding CPU annotation based on cpuRequest (integer value): %d", cpuRequest.Value())
		annotations[PEERPODS_CPU_ANNOTATION] = strconv.FormatInt(cpuRequest.Value(), 10)
	}

	// Add memory annotation
	if !memoryRequest.IsZero() && memoryLimit.Cmp(memoryRequest) >= 0 {
		if overhead.Memory() != nil {
			memoryLimit.Add(*overhead.Memory())
		}
		logger.Printf("Adding Memory annotation based on memoryLimit: %s", memoryLimit.String())
		memoryLimitMiBStr, err := utils.ConvertMemoryQuantityToMib(memoryLimit)
		if err != nil {
			logger.Printf("Error converting memory quantity to MiB: %v", err)
		}
		annotations[PEERPODS_MEMORY_ANNOTATION] = memoryLimitMiBStr

	} else if memoryRequest.Sign() == 1 {
		if overhead.Memory() != nil {
			memoryRequest.Add(*overhead.Memory())
		}
		logger.Printf("Adding Memory annotation based on memoryRequest: %s", memoryRequest.String())
		memoryRequestMiBStr, err := utils.ConvertMemoryQuantityToMib(memoryRequest)
		if err != nil {
			logger.Printf("Error converting memory quantity to MiB: %v", err)
		}
		annotations[PEERPODS_MEMORY_ANNOTATION] = memoryRequestMiBStr
	}

	// Add GPU annotation
	if gpuRequest.Sign() == 1 {
		logger.Printf("Adding GPU annotation based on gpuRequest: %s", gpuRequest.String())
		annotations[PEERPODS_GPU_ANNOTATION] = gpuRequest.String()
	}

	pod.SetAnnotations(annotations)

	// Remove all resource specs
	for idx := range pod.Spec.Containers {
		pod.Spec.Containers[idx].Resources = corev1.ResourceRequirements{}
	}

	for idx := range pod.Spec.InitContainers {
		pod.Spec.InitContainers[idx].Resources = corev1.ResourceRequirements{}
	}

	// Add peer-pod resource to one container
	pod.Spec.Containers[0].Resources = defaultContainerResourceRequirements()
	return pod
}

// defaultContainerResourceRequirements returns the default requirements for a container
func defaultContainerResourceRequirements() corev1.ResourceRequirements {
	requirements := corev1.ResourceRequirements{}
	requirements.Requests = corev1.ResourceList{}
	requirements.Limits = corev1.ResourceList{}

	var podVmExtResource string
	if podVmExtResource = os.Getenv("POD_VM_EXTENDED_RESOURCE"); podVmExtResource == "" {
		podVmExtResource = POD_VM_EXTENDED_RESOURCE_DEFAULT
	}

	requirements.Requests[corev1.ResourceName(podVmExtResource)] = resource.MustParse("1")
	requirements.Limits[corev1.ResourceName(podVmExtResource)] = resource.MustParse("1")
	return requirements
}
