# AWS EBS CSI Wrapper for Peer Pod Storage

## Prerequisites

* Running Kubernetes cluster (Version >= 1.20) on STACKIT

* Peer-Pods is [deployed](../../../cloud-api-adaptor/aws/README.md)

## CSI Driver Installation

Currently done in gardner

## Apply the PeerPods CSI wrapper

1. Target shoot cluster
1. Create the PeerpodVolume CRD object

```
kubectl apply -f ../../crd/peerpodvolume.yaml
```

3. Apply RBAC roles to permit the wrapper to execute the required operations

```
kubectl apply -f rbac-csi-wrapper.yaml
# this is the permission for the csi-driver running inside the podvm 
kubectl apply -f rbac-csi-wrapper-podvm.yaml
```

4. Ignore reconcilation of CSI managed resource

```
kubectl annotate managedresources.resources.gardener.cloud extension-controlplane-shoot resources.gardener.cloud
/ignore=true
```

5. Patch CSI node daemonset:

```
kubectl patch daemonsets.apps -n kube-system csi-driver-node --patch-file=patch-node.yaml
```

6. Target control-plane
7. Ignore reconcilation of Shoot to stop reconcilations of the control-plane:

```
kubectl annotate shoot <SHOOT> shoot.gardener.cloud/ignore=true
```

8. Patch the CSI Driver:

```
kubectl patch deployments.apps csi-driver-controller --patch-file patch-controller.yaml
kubectl scale deployment csi-driver-controller --replicas=1
```

## Example Workload With Provisioned Volume

This is based on the Dynamic Volume Provisioning [example](https://github.com/kubernetes-sigs/aws-ebs-csi-driver/tree/master/examples/kubernetes/dynamic-provisioning)

1. Deploy example pod on your cluster along with the StorageClass and PersistentVolumeClaim:

```

 kubectl apply -f dynamic-provisioning/

```

2. Validate the PersistentVolumeClaim is bound to your PersistentVolume:

```

kubectl get pvc peerpod-claim

```

3. Once the pod is running you can validate some date (timestamps) has been written to the dynamically provisioned volume:

```

kubectl exec app -- cat /data/out.txt

```

4. Cleanup resources:

```

kubectl delete -f dynamic-provisioning/

```
