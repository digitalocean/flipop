# FLIPOP - Floating IP Operator

## What?
This tool watches Kubernetes nodes and adjusts cloud network resources (floating IPs and DNS, currently) to target matching nodes. Nodes can be targeted based labels + taints and their pods (health, namespace, and labels).

## Why?
Kubernetes nodes and the pods they host are ephemeral and replaced in case of failure, update, or operational convenience. Kubernetes LoadBalancer type services are the traditional tool pivoting cluster traffic in these cases, but don't suit all workloads (ex. latency sensitive workloads, UDP, etc.). This tool aims to provide similar functionality through floating IPs and/or DNS.

## Config

### FloatingIPPool
```
apiVersion: flipop.digitalocean.com/v1alpha1
kind: FloatingIPPool
metadata:
  name: ingress-pool
spec: 
  provider: digitalocean
  region: nyc3
  desiredIPs: 3
  assignmentCoolOffSeconds: 20
  ips:
  - 192.168.1.1
  - 192.168.2.1
  dnsRecordSet:
    recordName: hello-world.example.com
    zone: abcdefghijklmnopqrstuvwxyz012345
    ttl: 30
    provider: cloudflare
  match:
    podNamespace: ingress
    podLabel: app=nginx-ingress,component=controller
    nodeLabel: doks.digitalocean.com/node-pool=work
    tolerations:
      - effect: NoSchedule
        key: node.kubernetes.io/unschedulable
```

### NodeDNSRecordSet
```
apiVersion: flipop.digitalocean.com/v1alpha1
kind: NodeDNSRecordSet
metadata:
  name: ingress-nodes
spec:
  dnsRecordSet:
    recordName: nodes
    zone: example.com
    ttl: 120
    provider: digitalocean
  match:
    podNamespace: ingress
    podLabel: app=nginx-ingress,component=controller
    nodeLabel: doks.digitalocean.com/node-pool=work
    tolerations:
      - effect: NoSchedule
        key: node.kubernetes.io/unschedulable
```

## Providers
Flipop supports DNS providers and Floating IP providers. FloatingIPPool resources require a Floating IP provider, and can optionally leverage an additional DNS provider. NodeDNSRecordSet providers require a DNS provider.
| Provider     | IP Provider | DNS Provider | Config                             |
|--------------|:-----------:|:------------:|------------------------------------|
| digitalocean |      X      |       X      | env var: DIGITALOCEAN_ACCESS_TOKEN |
| cloudflare   |             |       X      | env var: CLOUDFLARE_TOKEN          |

## Installation
```
kubectl create namespace flipop
kubectl create secret generic flipop -n flipop --from-literal=DIGITALOCEAN_ACCESS_TOKEN="CENSORED"
kubectl apply -n flipop -f k8s/*
```

## Why not operator-framework/kubebuilder?

This operator is concerned with the relationships between FloatingIPPool, Node, and Pod resources. The controller-runtime (leveraged by kubebuilder) and operator-framework assume related objects are owned by the controller objects. OwnerReferences trigger garbage collection, which is a non-starter for this use-case. Deleting a FloatingIPPool shouldn't delete the Pods and Nodes its concerned with. The controller-runtime also assumes we're interested in all resources we "own". While controllers can be constrained with label selectors and namespaces, controllers can only be added to manager, not removed. In the case of this controller, we're likely only interested a small subset of pods and nodes, but those subscriptions may change based upon the definition in the FloatingIPPool resource.

## TODO
- __Grace-periods__ - Moving IPs has a cost. It breaks all active connections, has a momentary period where connections will fail, and risks errors.  In some cases it may be better to give the node a chance to recover.

## Bugs / PRs / Contributing

At DigitalOcean we value and love our community! If you have any issues or would like to contribute, see [CONTRIBUTING.md](CONTRIBUTING.md).