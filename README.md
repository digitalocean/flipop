# FLIPOP - Floating IP Operator

## What?
This tool allocates and dynamically assigns floating IP addresses to Kubernetes nodes based upon criteria defined in a CustomerResourceDefinition.  Criteria for IP targeting can be based upon nodes (labels + taints) and their pods (health, namespace, and labels).

## Why?
Kubernetes nodes and the pods they host are ephemeral and replaced in case of failure, update, or operational convenience. As a counter-point, DNS moves slowly.  Floating IPs can pivot traffic at the speed of nodes and pods, without requiring DNS updates.

## Config

```
apiVersion: flipop.digitalocean.com/v1alpha1
kind: FloatingIPPool
metadata:
  name: ingress-pool
spec: 
  provider: digitalocean
  region: nyc3
  desiredIPs: 2
  ips:
  - 1.2.3.4
  - 5.6.7.8
  match:
    podNamespace: ingress
    podLabel: app=nginx-ingress,component=controller
    nodeLabel: doks.digitalocean.com/node-pool=ingress
```

## Installation
```
kubectl apply -f k8s/*
```

## Why not operator-framework/kubebuilder?

This operator is concerned with the relationships between FloatingIPPool, Node, and Pod resources. The controller-runtime (leveraged by kubebuilder) and operator-framework assume related objects are owned by the controller objects. OwnerReferences trigger garbage collection, which is a non-starter for this use-case. Deleting a FloatingIPPool shouldn't delete the Pods and Nodes its concerned with. The controller-runtime also assumes we're interested in all resources we "own". While controllers can be constrained with label selectors and namespaces, controllers can only be added to manager, not removed. In the case of this controller, we're likely only interested a small subset of pods and nodes, but those subscriptions may change based upon the definition in the FloatingIPPool resource.

## TODO
- __Grace-periods__ - Moving IPs has a cost. It breaks all active connections, has a momentary period where connections will fail, and risks errors.  In some cases it may be better to give the node a chance to recover.
- __DNS__ - 
- __RBAC__ - Right now this just grants ClusterAdmin which is not great.
- __Docker Repo__ - Need to create a docker repo.
