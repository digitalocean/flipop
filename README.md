# Floating IP Operator (FLIPOP)

FLIPOP is a Kubernetes operator that manages cloud-native Floating IPs (also referred to as Reserved IPs) and DNS records for targeted nodes and pods. It provides advanced traffic steering for workloads—especially latency-sensitive or UDP traffic—where built-in Kubernetes LoadBalancer services may not suffice.

---

## Features

* Assign and unassign Floating IPs to Kubernetes nodes based on pod and node selectors.
* Manage DNS A records containing floating or node IPs.
* Support for multiple DNS providers (e.g., DigitalOcean, Cloudflare).
* Expose rich Prometheus metrics for observability.
* Graceful reconciliation loops with configurable retry/backoff.
* Leader election for high-availability.

---

## Architecture

1. **CRD Watchers**: Informers monitor `FloatingIPPool` and `NodeDNSRecordSet` resources.
2. **Match Controller** (`nodematch`): Evaluates pods and nodes against label/taint-based criteria.
3. **IP Controller** (`ip_controller`): Reconciles Floating IP assignments and updates status & annotations.
4. **DNS Enabler/Disabler** (`nodedns`): Updates DNS records for matching nodes.
5. **Metrics Collector** (`metrics`): Implements Prometheus `Collector` interfaces for each controller.
6. **Leader Election** (`leaderelection`): Ensures only one active control loop per cluster.

---

## Custom Resources

### FloatingIPPool

Manage Floating IPs and optional DNS records for pods matching specified criteria.

```yaml
apiVersion: flipop.digitalocean.com/v1alpha1
kind: FloatingIPPool
metadata:
  name: ingress-pool
spec:
  provider: digitalocean          # IP provider
  region: nyc3                   # Cloud region
  desiredIPs: 3                  # Total IPs to allocate
  assignmentCoolOffSeconds: 20   # Seconds to wait between ip assignments, defaults to 0 if not set
  ips:                           # Static IP list (optional)
    - 192.168.1.1
    - 192.168.2.1
  dnsRecordSet:                  # Optional DNS configuration (defaults to digitalocean)
    recordName: hello
    zone:      example.com
    ttl:       30
    provider:  digitalocean
  match:                         # Node/pod matching criteria
    podNamespace: ingress
    podLabel:     app=nginx,component=controller
    nodeLabel:    doks.digitalocean.com/node-pool=work
    tolerations:
      - key:    node.kubernetes.io/unschedulable
        effect: NoSchedule
```

**Behavior**:

* Allocates a number of Floating IPs equal to `desiredIPs`.
  * By default, new floating IPs will be created
  * If you wish to use existing Floating IPs specify them in the list of `ips`
* Assigns IPs to matching nodes (see Matching section below)
* Updates DNS A record (if configured) using FloatingIPPool’s reserved IPs by default.
  * Note this behavior is slightly different than how `NodeDNSRecordSet` works. `dnsRecordSet` will always update the DNS record with the nodes Floating IP address, where `NodeDNSRecordSet` must be configured to use the Floating IP address.
* The annotation `flipop.digitalocean.com/ipv4-reserved-ip` is added to each node with the assigned Floating IP address as the value.

---

### NodeDNSRecordSet

Manage DNS A records for nodes matching specified criteria.

```yaml
apiVersion: flipop.digitalocean.com/v1alpha1
kind: NodeDNSRecordSet
metadata:
  name: ingress-nodes
spec:
  provider: digitalocean         # DNS provider (defaults to digitalocean)
  dnsRecordSet:
    recordName: nodes.example.com
    zone:      example.com
    ttl:       120
  addressType: flipop.digitalocean.com/ipv4-reserved-ip  # Use the node’s reserved IPv4 address (via annotation)
  match:
    nodeLabel:  doks.digitalocean.com/node-pool=work
    podNamespace: ingress
    podLabel:     app=nginx
    tolerations:
      - key:    node.kubernetes.io/unschedulable
        effect: NoSchedule
```

**Field**:

* `addressType`: Specifies which node address to publish in DNS. Options:
    * `ExternalIP` (default): Uses each node’s external/public IP.
    * `flipop.digitalocean.com/ipv4-reserved-ip`: Uses the node’s reserved IPv4 address assigned by a FloatingIPPool. Must be set explicitly when DNS should point to reserved IPs. When this addressType is specified that controller will look for the value of this annotation on each node to determine the reserved IP for the node.
  * `InternalIP`: Uses the node’s internal Kubernetes cluster IP.

**Behavior**:

* Watches nodes matching `match` criteria.
* Collects the specified address type from each node.
* Updates the DNS A record with the collected addresses.

---

## Matching Behavior

FLIPOP uses `spec.match` fields to determine which nodes receive Floating IPs:

1. **Pod Matching**: The controller watches pods in the specified `podNamespace` with labels matching `podLabel`. Only nodes running at least one matching pod are candidates.
2. **Node Matching**: Nodes are filtered by `nodeLabel` and `tolerations`. If a node’s labels and taints match, it passes the node filter.

**Assignment Logic**:

* On each reconciliation, the IP Controller collects all candidate nodes.
* If the number of assigned IPs is less than `desiredIPs`, it assigns IPs to the top candidates (sorted by name) until the quota is met.
* If nodes no longer host matching pods or no longer match node criteria, then the annotation is removed and any DNS records are updated.
  * Note that the controller will only unassign a Floating IP address from a Droplet if that node no longer matches AND it needs to assign the Floating IP to another node. This means that if a Floating IP is no longer needed it will stay attached to a Droplet to avoid any costs associated with a unassigned Floating IP address.
* Reassignments respect `assignmentCoolOffSeconds` to avoid rapid churn. 
* When assigning an IP, the controller:
    1. Requests an available IP from the provider or uses an assigned one from its list.
    2. Annotates the node with `flipop.digitalocean.com/ipv4-reserved-ip: <IP>`.
    3. Optionally updates DNS via `dnsRecordSet`.

---

## Metrics

FLIPOP exports Prometheus metrics for both controllers and underlying provider calls.

### FloatingIPPool Controller Metrics

Collected by `pkg/floatingip/metrics.go`:

* `flipop_floatingippoolcontroller_node_status{namespace,name,provider,dns,status}`: Gauge of node counts by status (`available`, `assigned`).
* `flipop_floatingippoolcontroller_ip_assignment_errors{namespace,name,ip,provider,dns}`: Counter of IP assignment failures.
* `flipop_floatingippoolcontroller_ip_assignments{namespace,name,ip,provider,dns}`: Counter of successful assignments.
* `flipop_floatingippoolcontroller_ip_node{namespace,name,ip,provider,dns,provider_id,node}`: Gauge mapping IP to node.
* `flipop_floatingippoolcontroller_ip_state{namespace,name,ip,provider,dns,state}`: Gauge of each IP’s current state.
* `flipop_floatingippoolcontroller_unfulfilled_ips{namespace,name,provider,dns}`: Gauge of desired minus actual acquired IPs.

### NodeDNSRecordSet Controller Metrics

Exposed via `pkg/nodedns/metrics.go`:

* `flipop_nodednsrecordset_records{namespace,name,provider,dns}`: Gauge of total DNS records managed.

### Provider Call Metrics

Each provider instruments calls in `pkg/provider/metrics.go`:

* `flipop_<subsystem>_calls_total{provider,call,outcome,kind,namespace,name}`: Counter of provider API invocations, labeled by outcome (`success` or `error`).
* `flipop_<subsystem>_call_duration_seconds{provider,call,kind,namespace,name}`: Histogram of call latencies.

---

## Providers

| Provider     | IP Provider | DNS Provider | Configuration               |
| ------------ | :---------: | :----------: | --------------------------- |
| digitalocean |      ✅      |       ✅      | `DIGITALOCEAN_ACCESS_TOKEN` |
| cloudflare   |      ❌      |       ✅      | `CLOUDFLARE_TOKEN`          |

Set credentials as environment variables in your operator namespace.

---

## Installation

1. Create a namespace and secrets:

   ```bash
   kubectl create namespace flipop
   kubectl create secret generic flipop -n flipop \
     --from-literal=DIGITALOCEAN_ACCESS_TOKEN="$DIGITALOCEAN_ACCESS_TOKEN" \
     --from-literal=CLOUDFLARE_TOKEN="$CLOUDFLARE_TOKEN"
   ```
2. Apply CRDs and controller manifests:

   ```bash
   kubectl apply -n flipop -f https://raw.githubusercontent.com/your-org/flipop/main/config/crd/ \
     && kubectl apply -n flipop -f https://raw.githubusercontent.com/your-org/flipop/main/config/manager/
   ```
3. Verify pods are running:

   ```bash
   kubectl -n flipop get pods
   ```

---

## Usage

1. Configure a `FloatingIPPool` or `NodeDNSRecordSet` as shown above.
2. Check status:

   ```bash
   kubectl describe floatingippool <name>
   kubectl describe nodednsrecordset <name>
   ```
3. Monitor metrics on Prometheus under namespaces `flipop_floatingippoolcontroller_*`, `flipop_nodednsrecordset_*`, and provider metrics under `flipop_<provider>_calls_total` and `flipop_<provider>_call_duration_seconds`.

---

## Development

* **Run unit tests**:

  ```bash
  go test ./pkg/... -cover
  ```
* **Linting**:

  ```bash
  golangci-lint run
  ```
* **Build/operator image**:

  ```bash
  make build-image
  ```

---

## Contributing

1. Fork the repo and create a feature branch.
2. Write tests for new behavior.
3. Submit a pull request with clear description and linked issue.
4. Ensure CI checks pass.

---

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
