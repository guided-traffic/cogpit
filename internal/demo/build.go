//nolint:goconst // Object builders: Kubernetes field names and values read best as literals, as in a manifest.
package demo

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const day = 24 * time.Hour

// Names. Generated objects use example.com registries and TEST-NET addresses, never real ones.
var (
	namespaceNames = []string{
		"default", "kube-system", MCPNamespace, "demo-shop", "demo-payments", "demo-search", "demo-auth",
		"demo-identity", "demo-web", "demo-data", "demo-ml", "demo-batch", "demo-monitoring", "demo-logging",
		"demo-cache", "demo-queue", "demo-mail", "demo-media", "demo-billing", "demo-legacy",
	}
	domains = []string{
		"cart", "checkout", "catalog", "search", "auth", "user", "order", "invoice", "payment", "shipping",
		"inventory", "pricing", "review", "recommend", "email", "sms", "image", "video", "report", "audit",
		"ledger", "profile", "session", "token", "feature", "config", "metrics", "trace", "alert", "billing",
	}
	roles = []string{
		"api", "web", "worker", "gateway", "frontend", "backend", "scheduler", "indexer", "sync", "proxy",
		"cache", "consumer", "producer", "notifier", "exporter", "renderer", "resolver", "collector",
		"processor", "router",
	}
	jobKinds = []string{"report", "cleanup", "sync", "backup", "digest", "export"}
	cpus     = []string{"50m", "100m", "250m", "500m", "1"}
	mems     = []string{"64Mi", "128Mi", "256Mi", "512Mi", "1Gi", "2Gi"}
)

// Well-known images; tags pinned.
const (
	imageNginx       = "nginx:1.30.0-alpine"
	imageAgnhost     = "registry.k8s.io/e2e-test-images/agnhost:2.53"
	imageBusybox     = "busybox:1.37.0"
	imageRedis       = "redis:8.2.1-alpine"
	imageMeshProxy   = "registry.example.com/platform/mesh-proxy:1.27.1"
	imageLogShipper  = "registry.example.com/platform/log-shipper:3.1.4"
	kubeVersion      = "v1.36.1"
	kubeVersionOld   = "v1.35.5"
	roleControlPlane = "control-plane"
	roleWorker       = "worker"
	roleGPU          = "gpu"
	protoTCP         = "TCP"
	portHTTP         = "http"
	restartAlways    = "Always"
	saDefault        = "default"
	kubeSystem       = "kube-system"
)

// ServerVersion is the version the synthetic cluster's API server reports: its kube-apiserver's
// image tag, newer than its oldest kubelets (a rolling upgrade, buildNodes).
const ServerVersion = kubeVersion

// build creates the initial cluster. Called with c.mu held.
func (c *Cluster) build() {
	now := c.opts.Now
	c.w.podKeys = map[string]struct{}{}
	rate := c.opts.EventsPerSecond
	if rate <= 0 {
		rate = DefaultEventsPerSecond
	}
	c.w.settle = int64(max(100, 2*rate))

	c.buildNamespaces(now)
	c.buildNodes(now)
	c.buildWorkloads(now)
	c.buildServices(now)
	c.buildEvents(now)
	c.buildHelmReleases(now) // last, and without the random generator: the rest stays as it was
}

// Namespaces.

func (c *Cluster) buildNamespaces(now time.Time) {
	n := c.opts.Namespaces
	for i := range n {
		name := fmt.Sprintf("demo-team-%02d", i-len(namespaceNames)+1)
		if i < len(namespaceNames) {
			name = namespaceNames[i]
		}
		ns := &namespace{name: name, uid: c.newUID(), created: now.Add(-30 * day)}
		ns.labels = map[string]any{"kubernetes.io/metadata.name": name}
		switch name {
		case kubeSystem:
		case saDefault:
			ns.weight = 1
		default:
			ns.created = c.between(now.Add(-30*day), now.Add(-25*day))
			ns.labels["team"] = strings.TrimPrefix(name, "demo-")
			ns.weight = 1 + c.rng.IntN(4)
		}
		if name == MCPNamespace {
			ns.weight = 1
		}
		c.w.namespaces = append(c.w.namespaces, ns)
	}
	// The last namespace is being deleted: it holds no workloads and shows Terminating.
	if n >= 5 {
		last := c.w.namespaces[n-1]
		last.terminating, last.weight = true, 0
	}
	for _, ns := range c.w.namespaces {
		c.put(kindNamespaces, c.renderNamespace(ns))
	}
}

// userNamespace picks a namespace for a user workload, weighted.
func (c *Cluster) userNamespace() *namespace {
	total := 0
	for _, ns := range c.w.namespaces {
		total += ns.weight
	}
	r := c.rng.IntN(total)
	for _, ns := range c.w.namespaces {
		if r < ns.weight {
			return ns
		}
		r -= ns.weight
	}
	return c.w.namespaces[0]
}

func (c *Cluster) namespace(name string) *namespace {
	for _, ns := range c.w.namespaces {
		if ns.name == name {
			return ns
		}
	}
	return nil
}

// Nodes.

func (c *Cluster) buildNodes(now time.Time) {
	total := c.opts.Nodes
	cps := 1
	if total >= 6 {
		cps = 3
	}
	cps = min(cps, total-1) // at least one worker
	workers := total - cps
	width := max(2, len(strconv.Itoa(workers)))
	for i := range total {
		n := &node{idx: i, uid: c.newUID(), arch: "amd64", kubelet: kubeVersion, nextIP: 2}
		if i < cps {
			n.name, n.role, n.size, n.cpu, n.memGi = fmt.Sprintf("cp-%d", i+1), roleControlPlane, "standard-4", 4, 16
		} else {
			w := i - cps + 1
			n.name, n.role, n.size, n.cpu, n.memGi = fmt.Sprintf("worker-%0*d", width, w), roleWorker, "standard-16", 16, 64
			switch {
			case workers >= 10 && w > workers-4:
				n.role, n.size, n.gpu = roleGPU, "gpu-16", 2
			case c.chance(0.25):
				n.arch, n.size = "arm64", "standard-16-arm"
			}
			if workers >= 10 && w > workers-9 && w <= workers-4 {
				n.kubelet = kubeVersionOld // a rolling upgrade in progress
			}
		}
		n.zone = "demo-1" + string("abc"[i%3])
		n.ip = fmt.Sprintf("10.0.%d.%d", 1+i/250, 10+i%250)
		n.podCIDR = fmt.Sprintf("10.%d.%d.0/23", 244+i/128, (i%128)*2)
		n.created = c.between(now.Add(-30*day), now.Add(-27*day))
		n.readySince = n.created.Add(40 * time.Second)
		n.heartbeat = now.Add(-time.Duration(c.rng.IntN(10)) * time.Second)
		c.w.nodes = append(c.w.nodes, n)
	}
	// One worker is NotReady under memory pressure, another is cordoned.
	if workers >= 3 {
		bad := c.w.nodes[cps+workers/3]
		bad.notReady, bad.readySince = true, now.Add(-47*time.Minute)
		bad.heartbeat = bad.readySince
		c.w.nodes[cps+workers/5].cordoned = true
	}
	for _, n := range c.w.nodes {
		c.renderNodeStatic(n)
		if n.role == roleWorker && !n.cordoned && !n.notReady {
			c.w.workers = append(c.w.workers, n)
		}
	}
	if len(c.w.workers) == 0 { // a tiny cluster: schedule on whatever there is
		c.w.workers = append(c.w.workers, c.w.nodes[len(c.w.nodes)-1])
	}
	for _, n := range c.w.nodes {
		c.put(kindNodes, c.renderNode(n))
	}
}

// placement returns the nodes the initial pods run on: the schedulable workers and the NotReady
// one, whose pods were there before it failed.
func (c *Cluster) placement() []*node {
	out := append([]*node(nil), c.w.workers...)
	for _, n := range c.w.nodes {
		if n.notReady {
			out = append(out, n)
		}
	}
	return out
}

func (c *Cluster) notReadyNode() *node {
	for _, n := range c.w.nodes {
		if n.notReady {
			return n
		}
	}
	return nil
}

// Pods.

// newPod creates a pod; the caller sets ownership and calls addPod.
func (c *Cluster) newPod(namespace, name string, t *podTemplate, n *node, phase podPhase, created time.Time) *pod {
	p := &pod{namespace: namespace, name: name, uid: c.newUID(), created: created, tmpl: t, node: n, phase: phase}
	p.spec = renderPodSpec(p)
	p.labels = strMap(t.labels)
	p.readySince = created
	if phase != phaseUnschedulable && phase != phaseCreating {
		p.started = created.Add(time.Duration(1+c.rng.IntN(5)) * time.Second)
		p.readySince = p.started.Add(time.Duration(2+c.rng.IntN(20)) * time.Second)
		if n != nil {
			p.ip = n.ip
			if !t.hostNetwork {
				p.ip = n.podIP()
			}
		}
	}
	p.ready = phase == phaseRunning
	return p
}

func (c *Cluster) addPod(p *pod) {
	p.idx = len(c.w.pods)
	c.w.pods = append(c.w.pods, p)
	c.w.podKeys[p.key()] = struct{}{}
	if p.deploy != nil {
		p.deploy.pods[p] = struct{}{}
	}
}

func (c *Cluster) removePod(p *pod) {
	last := c.w.pods[len(c.w.pods)-1]
	c.w.pods[p.idx] = last
	last.idx = p.idx
	c.w.pods = c.w.pods[:len(c.w.pods)-1]
	delete(c.w.podKeys, p.key())
	if p.deploy != nil {
		delete(p.deploy.pods, p)
	}
}

// uniqueName returns prefix plus a random suffix not used by another pod in the namespace.
func (c *Cluster) uniqueName(namespace, prefix string) string {
	for {
		name := prefix + c.suffix(5)
		if _, used := c.w.podKeys[objKey(namespace, name)]; !used {
			return name
		}
	}
}

func (c *Cluster) newDeployPod(d *deployment, rs int, phase podPhase, created time.Time, n *node) *pod {
	p := c.newPod(d.namespace, c.uniqueName(d.namespace, d.name+"-"+d.rsHash[rs]+"-"), d.templates[rs], n, phase, created)
	p.deploy, p.rs = d, rs
	p.labels, p.owners = d.podLabels[rs], d.podOwners[rs]
	c.addPod(p)
	return p
}

func ownerRef(apiVersion, kind, name, uid string) []any {
	ref := reference(apiVersion, kind, "", name, uid)
	ref["controller"], ref["blockOwnerDeletion"] = true, true
	return []any{ref}
}

// buildWorkloads creates the deployments and exactly Options.Pods pods.
func (c *Cluster) buildWorkloads(now time.Time) {
	o := c.opts
	remaining := o.Pods
	take := func(n int) int {
		n = max(0, min(n, remaining))
		remaining -= n
		return n
	}
	cps := 0
	for _, n := range c.w.nodes {
		if n.role == roleControlPlane {
			cps++
		}
	}
	static := take(4 * cps)
	proxies := take(len(c.w.nodes))
	terminating := take(o.Pods / 300)
	evicted := take(o.Pods / 1000)
	jobs := take(o.Pods * 3 / 100)
	standalone := take(o.Pods / 100)

	c.buildSystemPods(static, proxies)
	c.buildDeployments(now, remaining)
	standalone += c.buildExtras(now, terminating, evicted)
	c.buildJobs(now, jobs)
	c.buildStandalone(now, standalone)

	for _, d := range c.w.deploys {
		d.refresh(now)
		c.put(kindDeployments, c.renderDeployment(d))
	}
	for _, p := range c.w.pods {
		c.put(kindPods, c.renderPod(p))
	}
	c.w.notReadyTarget = max(1, len(c.w.notReady))
}

func (c *Cluster) buildSystemPods(static, proxies int) {
	components := []struct{ name, image, cpu string }{
		{"etcd", "registry.k8s.io/etcd:3.6.4-0", "100m"},
		{"kube-apiserver", "registry.k8s.io/kube-apiserver:" + kubeVersion, "250m"},
		{"kube-controller-manager", "registry.k8s.io/kube-controller-manager:" + kubeVersion, "200m"},
		{"kube-scheduler", "registry.k8s.io/kube-scheduler:" + kubeVersion, "100m"},
	}
	templates := make([]*podTemplate, len(components))
	for i, comp := range components {
		templates[i] = &podTemplate{
			labels:         map[string]string{"component": comp.name, "tier": roleControlPlane},
			containers:     []container{{name: comp.name, image: comp.image, requests: map[string]string{"cpu": comp.cpu}}},
			restartPolicy:  restartAlways,
			serviceAccount: saDefault,
			priorityClass:  "system-node-critical",
			priority:       2000001000,
			hostNetwork:    true,
			tolerations:    []any{map[string]any{"operator": "Exists", "effect": "NoExecute"}},
		}
		renderTemplate(templates[i])
	}
	for _, n := range c.w.nodes {
		if n.role != roleControlPlane {
			continue
		}
		for i, comp := range components {
			if static == 0 {
				break
			}
			static--
			p := c.newPod(kubeSystem, comp.name+"-"+n.name, templates[i], n, phaseRunning, n.created.Add(time.Minute))
			p.owners = ownerRef("v1", "Node", n.name, n.uid)
			c.addPod(p)
		}
	}

	dsUID := c.newUID()
	proxy := &podTemplate{
		labels:         map[string]string{"k8s-app": "kube-proxy", "controller-revision-hash": "7b5d9c8f64", "pod-template-generation": "1"},
		containers:     []container{{name: "kube-proxy", image: "registry.k8s.io/kube-proxy:" + kubeVersion}},
		restartPolicy:  restartAlways,
		serviceAccount: "kube-proxy",
		priorityClass:  "system-node-critical",
		priority:       2000001000,
		hostNetwork:    true,
		tolerations:    []any{map[string]any{"operator": "Exists"}},
	}
	renderTemplate(proxy)
	owners := ownerRef("apps/v1", "DaemonSet", "kube-proxy", dsUID)
	for _, n := range c.w.nodes[:proxies] {
		p := c.newPod(kubeSystem, c.uniqueName(kubeSystem, "kube-proxy-"), proxy, n, phaseRunning, n.created.Add(2*time.Minute))
		p.owners = owners
		c.addPod(p)
	}
}

// Deployments.

// userTemplate is the pod template of a generated application: one main container and up to two
// sidecars, with resources that give a mix of QoS classes.
func (c *Cluster) userTemplate(ns, app, role string) *podTemplate {
	team := strings.TrimPrefix(ns, "demo-")
	main := container{
		name:  app,
		image: fmt.Sprintf("registry.example.com/%s/%s:%d.%d.%d", team, app, 1+c.rng.IntN(3), c.rng.IntN(20), c.rng.IntN(10)),
		ports: []containerPort{{name: portHTTP, port: 8080, protocol: protoTCP}},
	}
	if role == "cache" {
		main.image = imageRedis
		main.ports = []containerPort{{name: "redis", port: 6379, protocol: protoTCP}}
	}
	if c.chance(0.5) {
		main.ports = append(main.ports, containerPort{name: "metrics", port: 9090, protocol: protoTCP})
	}
	cs := []container{main}
	switch u := c.rng.Float64(); {
	case u < 0.10:
		cs = append(cs,
			container{name: "mesh-proxy", image: imageMeshProxy, ports: []containerPort{{name: "proxy-metrics", port: 15090, protocol: protoTCP}}},
			container{name: "log-shipper", image: imageLogShipper})
	case u < 0.40:
		cs = append(cs, container{name: "mesh-proxy", image: imageMeshProxy, ports: []containerPort{{name: "proxy-metrics", port: 15090, protocol: protoTCP}}})
	}
	qos := c.rng.Float64()
	for i := range cs {
		cpu, mem := c.rng.IntN(len(cpus)), c.rng.IntN(len(mems)-1)
		if i > 0 {
			cpu, mem = 0, 0 // sidecars are small
		}
		switch {
		case qos < 0.2: // Guaranteed
			cs[i].requests = map[string]string{"cpu": cpus[cpu], "memory": mems[mem]}
			cs[i].limits = map[string]string{"cpu": cpus[cpu], "memory": mems[mem]}
		case qos < 0.9: // Burstable
			cs[i].requests = map[string]string{"cpu": cpus[cpu], "memory": mems[mem]}
			cs[i].limits = map[string]string{"memory": mems[mem+1]}
		}
	}
	t := &podTemplate{
		labels:         map[string]string{appLabel: app},
		containers:     cs,
		restartPolicy:  restartAlways,
		serviceAccount: saDefault,
		tolerations:    defaultTolerations,
	}
	renderTemplate(t)
	return t
}

// fixedTemplate is the template of a well-known deployment.
func fixedTemplate(labels map[string]string, name, image string, ports []containerPort, sa string) *podTemplate {
	t := &podTemplate{
		labels: labels,
		containers: []container{{
			name: name, image: image, ports: ports,
			requests: map[string]string{"cpu": "100m", "memory": "70Mi"},
			limits:   map[string]string{"memory": "170Mi"},
		}},
		restartPolicy:  restartAlways,
		serviceAccount: sa,
		tolerations:    defaultTolerations,
	}
	renderTemplate(t)
	return t
}

func (c *Cluster) newDeployment(ns *namespace, name string, t *podTemplate, replicas int, now time.Time) *deployment {
	d := &deployment{
		name: name, namespace: ns.name, uid: c.newUID(), replicas: replicas,
		generation: int64(1 + c.rng.IntN(5)),
		pods:       map[*pod]struct{}{},
		selector:   t.labels,
	}
	d.created = c.between(ns.created.Add(time.Hour), now.Add(-time.Hour))
	d.templates[0] = t
	d.rsHash[0] = c.suffix(10)
	d.rsUID[0] = c.newUID()
	c.w.deploys = append(c.w.deploys, d)
	return d
}

// finishDeployment renders what a deployment's pods share and its spec.
func (c *Cluster) finishDeployment(d *deployment) {
	for rs := range d.templates {
		if d.templates[rs] == nil {
			continue
		}
		labels := strMap(d.templates[rs].labels)
		labels["pod-template-hash"] = d.rsHash[rs]
		d.podLabels[rs] = labels
		d.podOwners[rs] = ownerRef("apps/v1", "ReplicaSet", d.name+"-"+d.rsHash[rs], d.rsUID[rs])
	}
	c.renderDeploymentSpec(d)
}

// buildDeployments creates Options.Deployments deployments whose pods number exactly budget.
func (c *Cluster) buildDeployments(now time.Time, budget int) {
	type fixed struct {
		ns, name string
		replicas int
		t        *podTemplate
	}
	dnsPorts := []containerPort{{name: "dns", port: 53, protocol: "UDP"}, {name: "dns-tcp", port: 53, protocol: protoTCP}, {name: "metrics", port: 9153, protocol: protoTCP}}
	fixedOnes := []fixed{
		{kubeSystem, "coredns", 2, fixedTemplate(map[string]string{"k8s-app": "kube-dns"}, "coredns", "registry.k8s.io/coredns/coredns:v1.12.1", dnsPorts, "coredns")},
		{kubeSystem, "metrics-server", 1, fixedTemplate(map[string]string{"k8s-app": "metrics-server"}, "metrics-server", "registry.k8s.io/metrics-server/metrics-server:v0.8.0", []containerPort{{name: "https", port: 10250, protocol: protoTCP}}, "metrics-server")},
		{MCPNamespace, MCPService, 3, fixedTemplate(map[string]string{appLabel: MCPService}, "nginx", imageNginx, []containerPort{{name: portHTTP, port: 80, protocol: protoTCP}}, saDefault)},
		{MCPNamespace, "api", 2, fixedTemplate(map[string]string{appLabel: "api"}, "netexec", imageAgnhost, []containerPort{{name: portHTTP, port: 8080, protocol: protoTCP}}, saDefault)},
	}
	if len(fixedOnes) > c.opts.Deployments {
		fixedOnes = fixedOnes[:c.opts.Deployments]
	}
	for _, f := range fixedOnes {
		r := min(f.replicas, budget)
		budget -= r
		d := c.newDeployment(c.namespace(f.ns), f.name, f.t, r, now)
		d.created = c.opts.Now.Add(-29 * day)
	}

	// User deployments: draw replica counts from a skewed distribution, pick a profile for the
	// small ones, then scale the counts so the pods add up to the budget.
	user := make([]*deployment, 0, max(0, c.opts.Deployments-len(fixedOnes)))
	used := map[string]bool{}
	for range c.opts.Deployments - len(fixedOnes) {
		ns := c.userNamespace()
		domain, role := pick(c, domains), pick(c, roles)
		name := domain + "-" + role
		for i := 2; used[ns.name+"/"+name]; i++ {
			name = fmt.Sprintf("%s-%s-%d", domain, role, i)
		}
		used[ns.name+"/"+name] = true
		var r int
		switch u := c.rng.Float64(); {
		case u < 0.02:
			r = 0
		case u < 0.50:
			r = 1 + c.rng.IntN(3)
		case u < 0.80:
			r = 4 + c.rng.IntN(7)
		case u < 0.95:
			r = 11 + c.rng.IntN(30)
		default:
			r = 41 + c.rng.IntN(80)
		}
		d := c.newDeployment(ns, name, c.userTemplate(ns.name, name, role), r, now)
		if r >= 1 && r <= 10 {
			switch u := c.rng.Float64(); {
			case u < 0.05:
				d.profile = profileCrash
			case u < 0.09 && r >= 2:
				d.profile = profileBadImage
			case u < 0.12:
				d.profile = profilePending
			}
		}
		if d.profile == profileBadImage {
			// A stuck rollout: a new replica set with an image tag that does not exist, one
			// surge pod; the old replica set keeps serving.
			t := *d.templates[0]
			t.containers = append([]container(nil), t.containers...)
			t.containers[mainContainer].image += "-rc.1"
			renderTemplate(&t)
			d.templates[1], d.rsHash[1], d.rsUID[1], d.current = &t, c.suffix(10), c.newUID(), 1
			d.generation++
			budget-- // the surge pod
		}
		user = append(user, d)
	}
	c.scaleReplicas(user, max(0, budget))

	placement := c.placement()
	for _, d := range c.w.deploys {
		c.finishDeployment(d)
		c.deploymentPods(d, now, placement)
	}
}

// scaleReplicas scales the replica counts of ds (keeping zeros) so they sum to total.
func (c *Cluster) scaleReplicas(ds []*deployment, total int) {
	if len(ds) == 0 {
		return
	}
	sum := 0
	for _, d := range ds {
		sum += d.replicas
	}
	got := 0
	for _, d := range ds {
		if d.replicas > 0 {
			d.replicas = max(1, (d.replicas*total+sum/2)/sum)
		}
		got += d.replicas
	}
	// Spread the rounding difference randomly, then settle any rest in order.
	for range 10 * len(ds) {
		if got == total {
			return
		}
		d := ds[c.rng.IntN(len(ds))]
		switch {
		case got < total && d.replicas > 0:
			d.replicas++
			got++
		case got > total && d.replicas > 1:
			d.replicas--
			got--
		}
	}
	for i := 0; got != total; i = (i + 1) % len(ds) {
		d := ds[i]
		switch {
		case got < total:
			d.replicas++
			got++
		case d.replicas > 0:
			d.replicas--
			got--
		}
	}
}

// deploymentPods creates a deployment's initial pods according to its profile.
func (c *Cluster) deploymentPods(d *deployment, now time.Time, placement []*node) {
	recent := func(maxAge time.Duration) time.Time { return c.between(maxTime(d.created, now.Add(-maxAge)), now) }
	switch d.profile {
	case profileCrash:
		for range d.replicas {
			p := c.newDeployPod(d, 0, phaseCrashLoop, recent(3*day), pick(c, placement))
			p.restarts = max(5, int64(now.Sub(p.created)/(5*time.Minute)))
			p.lastExit = now.Add(-time.Duration(c.rng.IntN(300)) * time.Second)
			p.lastStart = p.lastExit.Add(-time.Duration(5+c.rng.IntN(25)) * time.Second)
			p.inBackOff = c.chance(0.8)
			if !p.inBackOff {
				p.lastStart = p.lastExit.Add(time.Duration(10+c.rng.IntN(20)) * time.Second)
			}
			c.w.unstable = append(c.w.unstable, p)
		}
	case profilePending:
		for range d.replicas {
			c.newDeployPod(d, 0, phaseUnschedulable, recent(2*day), nil)
		}
	default:
		for range d.replicas {
			c.healthyPod(d, now, placement)
		}
		if d.profile == profileBadImage {
			p := c.newDeployPod(d, 1, phaseImagePull, now.Add(-time.Duration(10+c.rng.IntN(110))*time.Minute), pick(c, placement))
			p.pullBackOff = c.chance(0.7)
			c.w.unstable = append(c.w.unstable, p)
		}
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// healthyPod creates a pod of a healthy replica set: mostly running and ready, some failing their
// readiness probe, a few still being created.
func (c *Cluster) healthyPod(d *deployment, now time.Time, placement []*node) {
	switch u := c.rng.Float64(); {
	case u < 0.01:
		p := c.newDeployPod(d, 0, phaseCreating, now.Add(-time.Duration(2+c.rng.IntN(20))*time.Second), pick(c, c.w.workers))
		c.w.creating = append(c.w.creating, p)
	default:
		p := c.newDeployPod(d, 0, phaseRunning, c.between(d.created, now.Add(-time.Minute)), pick(c, placement))
		switch v := c.rng.Float64(); {
		case v < 0.85:
		case v < 0.95:
			p.restarts = int64(1 + c.rng.IntN(3))
		default:
			p.restarts = int64(4 + c.rng.IntN(17))
		}
		if u < 0.025 {
			p.ready = false
			p.readySince = now.Add(-time.Duration(c.rng.IntN(600)) * time.Second)
			c.w.notReady = append(c.w.notReady, p)
		}
	}
}

// buildExtras creates terminating and evicted pods of healthy deployments. It returns the pods it
// could not place (a cluster without healthy deployments).
func (c *Cluster) buildExtras(now time.Time, terminating, evicted int) int {
	var healthy []*deployment
	for _, d := range c.w.deploys {
		if d.profile == profileHealthy && d.replicas > 0 && d.namespace != kubeSystem {
			healthy = append(healthy, d)
		}
	}
	if len(healthy) == 0 {
		return terminating + evicted
	}
	placement := c.placement()
	for range terminating {
		d := pick(c, healthy)
		p := c.newDeployPod(d, 0, phaseRunning, c.between(d.created, now.Add(-time.Hour)), pick(c, placement))
		p.deleting = now.Add(-time.Duration(1+c.rng.IntN(20)) * time.Second)
		c.w.terminating = append(c.w.terminating, p)
	}
	host := c.notReadyNode()
	for range evicted {
		d := pick(c, healthy)
		n := host
		if n == nil {
			n = pick(c, placement)
		}
		p := c.newDeployPod(d, 0, phaseEvicted, c.between(now.Add(-2*day), now.Add(-time.Hour)), n)
		p.lastExit = now.Add(-time.Duration(45+c.rng.IntN(5)) * time.Minute)
	}
	return 0
}

// buildJobs creates the pods of CronJob runs: about ten runs a CronJob, hourly, most completed.
func (c *Cluster) buildJobs(now time.Time, budget int) {
	if budget == 0 {
		return
	}
	crons := max(1, budget/10)
	used := map[string]bool{}
	for i := range crons {
		ns := c.userNamespace()
		name := pick(c, domains) + "-" + pick(c, jobKinds)
		for j := 2; used[ns.name+"/"+name]; j++ {
			name = fmt.Sprintf("%s-%d", name, j)
		}
		used[ns.name+"/"+name] = true
		runs := budget / crons
		if i < budget%crons {
			runs++
		}
		t := &podTemplate{
			labels: map[string]string{},
			containers: []container{{
				name:     name,
				image:    fmt.Sprintf("registry.example.com/%s/%s:1.%d.%d", strings.TrimPrefix(ns.name, "demo-"), name, c.rng.IntN(10), c.rng.IntN(10)),
				requests: map[string]string{"cpu": "100m", "memory": "128Mi"},
				limits:   map[string]string{"memory": "256Mi"},
			}},
			restartPolicy:  "Never",
			serviceAccount: saDefault,
			tolerations:    defaultTolerations,
		}
		renderTemplate(t)
		for r := range runs {
			scheduled := now.Truncate(time.Hour).Add(-time.Duration(r+1) * time.Hour)
			job := fmt.Sprintf("%s-%d", name, scheduled.Unix()/60)
			uid := c.newUID()
			phase := phaseSucceeded
			if c.chance(0.15) {
				phase = phaseFailed
			}
			p := c.newPod(ns.name, c.uniqueName(ns.name, job+"-"), t, pick(c, c.w.workers), phase, scheduled.Add(time.Second))
			p.labels = map[string]any{
				"batch.kubernetes.io/controller-uid": uid, "batch.kubernetes.io/job-name": job,
				"controller-uid": uid, "job-name": job,
			}
			p.owners = ownerRef("batch/v1", "Job", job, uid)
			p.lastExit = p.started.Add(time.Duration(20+c.rng.IntN(280)) * time.Second)
			c.addPod(p)
		}
	}
}

// buildStandalone creates pods without an owner: debugging and test pods people start by hand.
func (c *Cluster) buildStandalone(now time.Time, budget int) {
	kinds := []struct{ prefix, image string }{
		{"debug", imageBusybox},
		{"toolbox", imageBusybox},
		{"netshoot", "nicolaka/netshoot:v0.14"},
		{"curl", "curlimages/curl:8.16.0"},
		{"load-generator", imageAgnhost},
	}
	templates := make([]*podTemplate, len(kinds))
	for i, k := range kinds {
		templates[i] = &podTemplate{
			labels:         map[string]string{"run": k.prefix},
			containers:     []container{{name: k.prefix, image: k.image}},
			restartPolicy:  "Never",
			serviceAccount: saDefault,
			tolerations:    defaultTolerations,
		}
		renderTemplate(templates[i])
	}
	for range budget {
		i := c.rng.IntN(len(kinds))
		ns := c.userNamespace()
		phase := phaseRunning
		switch u := c.rng.Float64(); {
		case u < 0.15:
			phase = phaseSucceeded
		case u < 0.25:
			phase = phaseFailed
		}
		p := c.newPod(ns.name, c.uniqueName(ns.name, kinds[i].prefix+"-"), templates[i], pick(c, c.w.workers), phase, c.between(now.Add(-5*day), now.Add(-time.Minute)))
		p.lastExit = p.started.Add(time.Duration(10+c.rng.IntN(600)) * time.Second)
		c.addPod(p)
	}
}

// Services.

type serviceSpec struct {
	namespace, name string
	created         time.Time
	typ             string
	clusterIP       string
	ports           []any
	selector        map[string]string
	ingress         string // a LoadBalancer's address; empty while pending
	externalName    string
}

func (c *Cluster) buildServices(now time.Time) {
	usedIPs := map[string]bool{"10.96.0.1": true, "10.96.0.10": true, MCPClusterIP: true}
	nodePorts := map[int64]bool{}
	created := now.Add(-30 * day)
	var specs []serviceSpec
	add := func(s serviceSpec) {
		if c.namespace(s.namespace) != nil {
			specs = append(specs, s)
		}
	}
	add(serviceSpec{namespace: saDefault, name: "kubernetes", created: created, typ: "ClusterIP", clusterIP: "10.96.0.1",
		ports: []any{map[string]any{"name": "https", "port": int64(443), "targetPort": int64(6443), "protocol": protoTCP}}})

	var user []*deployment
	for _, d := range c.w.deploys {
		switch {
		case d.namespace == kubeSystem && d.name == "coredns":
			add(serviceSpec{namespace: kubeSystem, name: "kube-dns", created: created, typ: "ClusterIP", clusterIP: "10.96.0.10",
				ports: c.servicePorts(d, false, nodePorts), selector: d.selector})
		case d.namespace == kubeSystem:
			add(serviceSpec{namespace: kubeSystem, name: d.name, created: d.created, typ: "ClusterIP", clusterIP: c.clusterIP(usedIPs),
				ports: []any{map[string]any{"name": "https", "port": int64(443), "targetPort": int64(10250), "protocol": protoTCP}}, selector: d.selector})
		case d.namespace == MCPNamespace && d.name == MCPService:
			add(serviceSpec{namespace: MCPNamespace, name: MCPService, created: d.created, typ: "ClusterIP", clusterIP: MCPClusterIP,
				ports: c.servicePorts(d, false, nodePorts), selector: d.selector})
		case d.namespace == MCPNamespace && d.name == "api":
			add(serviceSpec{namespace: MCPNamespace, name: "api", created: d.created, typ: "ClusterIP", clusterIP: c.clusterIP(usedIPs),
				ports: c.servicePorts(d, false, nodePorts), selector: d.selector})
		default:
			user = append(user, d)
		}
	}
	c.rng.Shuffle(len(user), func(i, j int) { user[i], user[j] = user[j], user[i] })
	lb := 10
	for i := 0; len(specs) < c.opts.Services; i++ {
		if i >= len(user) {
			ns := c.userNamespace()
			n := i - len(user) + 1
			add(serviceSpec{namespace: ns.name, name: fmt.Sprintf("external-db-%d", n), created: c.between(ns.created, now), typ: "ExternalName",
				externalName: fmt.Sprintf("db-%d.%s.example.com", n, strings.TrimPrefix(ns.name, "demo-"))})
			continue
		}
		d := user[i]
		s := serviceSpec{namespace: d.namespace, name: d.name, created: d.created.Add(time.Minute), typ: "ClusterIP", selector: d.selector}
		switch u := c.rng.Float64(); {
		case u < 0.80:
			s.clusterIP = c.clusterIP(usedIPs)
		case u < 0.90:
			s.typ, s.clusterIP = "NodePort", c.clusterIP(usedIPs)
		case u < 0.96:
			s.typ, s.clusterIP = "LoadBalancer", c.clusterIP(usedIPs)
			if c.chance(0.75) {
				s.ingress = fmt.Sprintf("203.0.113.%d", lb)
				lb++
			}
		default:
			s.clusterIP = "None" // headless
		}
		s.ports = c.servicePorts(d, s.typ != "ClusterIP", nodePorts)
		add(s)
	}
	for _, s := range specs {
		c.put(kindServices, c.renderService(s))
	}
}

// clusterIP allocates a free address in the service CIDR 10.96.0.0/16.
func (c *Cluster) clusterIP(used map[string]bool) string {
	for {
		ip := fmt.Sprintf("10.96.%d.%d", c.rng.IntN(256), 1+c.rng.IntN(254))
		if !used[ip] {
			used[ip] = true
			return ip
		}
	}
}

// servicePorts exposes the main container's ports: HTTP on 80, the rest on their own numbers.
func (c *Cluster) servicePorts(d *deployment, nodePort bool, used map[int64]bool) []any {
	ports := d.templates[0].containers[mainContainer].ports
	out := make([]any, 0, len(ports))
	for _, p := range ports {
		port := p.port
		if p.name == portHTTP {
			port = 80
		}
		m := map[string]any{"name": p.name, "port": port, "targetPort": p.port, "protocol": p.protocol}
		if nodePort {
			for {
				np := int64(30000 + c.rng.IntN(2768))
				if !used[np] {
					used[np] = true
					m["nodePort"] = np
					break
				}
			}
		}
		out = append(out, m)
	}
	return out
}

func (c *Cluster) renderService(s serviceSpec) *unstructured.Unstructured {
	meta := map[string]any{
		"name": s.name, "namespace": s.namespace, "uid": c.newUID(), "creationTimestamp": ts(s.created),
		"labels": map[string]any{appLabel: s.name},
	}
	spec := map[string]any{"type": s.typ, "sessionAffinity": "None"}
	if s.typ == "ExternalName" {
		spec["externalName"] = s.externalName
	} else {
		spec["clusterIP"] = s.clusterIP
		spec["clusterIPs"] = []any{s.clusterIP}
		spec["ipFamilies"] = []any{"IPv4"}
		spec["ipFamilyPolicy"] = "SingleStack"
		spec["internalTrafficPolicy"] = "Cluster"
		spec["ports"] = s.ports
	}
	if s.selector != nil {
		spec["selector"] = strMap(s.selector)
	}
	if s.typ == "NodePort" || s.typ == "LoadBalancer" {
		spec["externalTrafficPolicy"] = "Cluster"
	}
	lb := map[string]any{}
	if s.typ == "LoadBalancer" {
		spec["allocateLoadBalancerNodePorts"] = true
		if s.ingress != "" {
			lb["ingress"] = []any{map[string]any{"ip": s.ingress, "ipMode": "VIP"}}
		}
	}
	return object("v1", "Service", meta, map[string]any{"spec": spec, "status": map[string]any{"loadBalancer": lb}})
}

// Events.

// buildEvents creates Options.Events events over the last hour, oldest first.
func (c *Cluster) buildEvents(now time.Time) {
	n := c.opts.Events
	start := now.Add(-time.Hour)
	for i := range n {
		last := start.Add(time.Duration(int64(i) * int64(time.Hour) / int64(n)))
		e := c.newEvent(last)
		c.w.events = append(c.w.events, e)
		c.put(kindEvents, c.renderEvent(e))
	}
}
