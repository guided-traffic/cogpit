//nolint:goconst // Chart values and templates: Kubernetes field names and values read best as literals, as in a chart.
package demo

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"text/template"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"sigs.k8s.io/yaml"
)

// The charts the demo's Helm releases are made from (helm.go): each version with its default
// values and its templates, rendered against a revision's values as Helm renders a chart, so that
// a revision's manifest follows from its values. The templates use a few of the functions Helm's
// do (toYaml, nindent, quote, b64enc, sha256sum, default) and read .Release, .Chart and .Values as
// Helm's do; .Labels and .Selector are the chart's standard labels, which a chart made with
// `helm create` defines in its _helpers.tpl, and .Rendered holds the files rendered before, for a
// checksum annotation. Registries are example.com's, as everywhere in the demo.

// demoChart is one version of a chart.
type demoChart struct {
	meta      *chart.Metadata
	values    map[string]any  // its defaults: the chart's values.yaml
	templates []chartTemplate // in the order Helm installs their objects
	notes     *template.Template
}

// chartTemplate is one file of a chart's templates/ directory.
type chartTemplate struct {
	file string // relative to templates/
	tmpl *template.Template
}

// renderData is what a template sees.
type renderData struct {
	Release  renderRelease
	Chart    *chart.Metadata
	Values   map[string]any
	Labels   string            // the chart's standard labels, one per line
	Selector string            // the labels its Services and controllers select by
	Rendered map[string]string // the files rendered so far, by their name under templates/
}

type renderRelease struct {
	Name, Namespace string
	Revision        int
	Service         string // helmService
}

// helmService is a release's .Release.Service, and the app.kubernetes.io/managed-by label Helm's
// charts give their objects.
const helmService = "Helm"

// render renders the chart for one revision of a release: its manifest, the documents as Helm
// stores them ("---", a "# Source:" line, the document), and its notes.
func (ch *demoChart) render(release, namespace string, revision int, config map[string]any) (manifest, notes string, err error) {
	d := renderData{
		Release:  renderRelease{Name: release, Namespace: namespace, Revision: revision, Service: helmService},
		Chart:    ch.meta,
		Values:   coalesce(ch.values, config),
		Labels:   chartLabels(ch.meta, release),
		Selector: selectorLabels(ch.meta, release),
		Rendered: map[string]string{},
	}
	var b strings.Builder
	for _, t := range ch.templates {
		var out strings.Builder
		if err := t.tmpl.Execute(&out, d); err != nil {
			return "", "", fmt.Errorf("%s/templates/%s: %w", ch.meta.Name, t.file, err)
		}
		doc := strings.TrimSpace(out.String())
		d.Rendered[t.file] = doc
		if doc == "" { // a template whose feature is off: Helm leaves it out
			continue
		}
		fmt.Fprintf(&b, "---\n# Source: %s/templates/%s\n%s\n", ch.meta.Name, t.file, doc)
	}
	var out strings.Builder
	if err := ch.notes.Execute(&out, d); err != nil {
		return "", "", fmt.Errorf("%s/templates/NOTES.txt: %w", ch.meta.Name, err)
	}
	return b.String(), strings.TrimSpace(out.String()) + "\n", nil
}

// coalesce returns a chart's defaults with the user-supplied values over them, as Helm merges
// them: maps merge key by key, any other value replaces the default. Neither input changes.
func coalesce(defaults, user map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(user))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range user {
		if um, ok := v.(map[string]any); ok {
			if dm, ok := out[k].(map[string]any); ok {
				out[k] = coalesce(dm, um)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// chartLabels are the labels a chart made with `helm create` puts on every object.
func chartLabels(m *chart.Metadata, release string) string {
	return "helm.sh/chart: " + m.Name + "-" + m.Version + "\n" + selectorLabels(m, release) + "\n" +
		"app.kubernetes.io/version: " + strconv.Quote(m.AppVersion) + "\n" +
		labelManagedBy + ": " + helmService
}

func selectorLabels(m *chart.Metadata, release string) string {
	return "app.kubernetes.io/name: " + m.Name + "\napp.kubernetes.io/instance: " + release
}

// chartFuncs are the template functions the demo's charts use, as Helm's (sprig's) behave.
var chartFuncs = template.FuncMap{
	"toYaml": func(v any) (string, error) {
		b, err := yaml.Marshal(v)
		return strings.TrimSuffix(string(b), "\n"), err
	},
	"nindent": func(n int, s string) string {
		pad := strings.Repeat(" ", n)
		return "\n" + pad + strings.ReplaceAll(s, "\n", "\n"+pad)
	},
	"quote":     func(v any) string { return strconv.Quote(fmt.Sprint(v)) },
	"b64enc":    func(v any) string { return base64.StdEncoding.EncodeToString([]byte(fmt.Sprint(v))) },
	"sha256sum": func(s string) string { return digest(s) },
	"default": func(def, v any) any {
		if v == nil || reflect.ValueOf(v).IsZero() {
			return def
		}
		return v
	},
}

// parseTemplate parses a chart's template. The templates are this file's constants, so a mistake in one
// is a bug every test of the package finds.
func parseTemplate(name, text string) *template.Template {
	return template.Must(template.New(name).Funcs(chartFuncs).Option("missingkey=zero").Parse(text))
}

func chartFiles(pairs ...string) []chartTemplate {
	out := make([]chartTemplate, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, chartTemplate{file: pairs[i], tmpl: parseTemplate(pairs[i], pairs[i+1])})
	}
	return out
}

func chartMeta(name, version, appVersion, description string) *chart.Metadata {
	return &chart.Metadata{
		APIVersion: chart.APIVersionV2, Name: name, Version: version, AppVersion: appVersion,
		Description: description, Type: "application", Home: "https://charts.example.com/" + name,
	}
}

// Templates more than one chart shares.
const (
	tmplServiceHTTP = `apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}
  labels:{{ .Labels | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: http
      protocol: TCP
      name: http
  selector:{{ .Selector | nindent 4 }}`
)

// shop-web: the shop's web front end. 1.5.0 adds a ConfigMap of its settings and moves the
// metrics port from a Service of its own to the main one.

const shopWebImage = "registry.example.com/shop/web"

func shopWebDefaults(v15 bool) map[string]any {
	v := map[string]any{
		"replicaCount":       1,
		"image":              map[string]any{"repository": shopWebImage, "tag": "", "pullPolicy": "IfNotPresent"},
		"imagePullSecrets":   []any{},
		"nameOverride":       "",
		"fullnameOverride":   "",
		"serviceAccount":     map[string]any{"create": true, "annotations": map[string]any{}, "name": ""},
		"podAnnotations":     map[string]any{},
		"podSecurityContext": map[string]any{"runAsNonRoot": true, "fsGroup": 2000},
		"securityContext":    map[string]any{"readOnlyRootFilesystem": true, "allowPrivilegeEscalation": false},
		"service":            map[string]any{"type": "ClusterIP", "port": 80},
		"ingress": map[string]any{
			"enabled": false, "className": "nginx", "annotations": map[string]any{}, "hosts": []any{}, "tls": []any{},
		},
		"resources": map[string]any{},
		"autoscaling": map[string]any{
			"enabled": false, "minReplicas": 1, "maxReplicas": 10, "targetCPUUtilizationPercentage": 80,
		},
		"nodeSelector": map[string]any{},
		"tolerations":  []any{},
		"affinity":     map[string]any{},
		"database": map[string]any{
			"host": "postgres.demo-shop.svc.cluster.local", "port": 5432, "name": "shop", "username": "shop", "password": "",
		},
		"env":    map[string]any{},
		"probes": map[string]any{"liveness": "/healthz", "readiness": "/ready"},
	}
	if v15 {
		v["metrics"] = map[string]any{"enabled": true, "port": 9090}
		v["config"] = map[string]any{
			"logLevel":     "info",
			"featureFlags": map[string]any{"newCheckout": false, "recommendations": true},
		}
	} else {
		v["metrics"] = map[string]any{"enabled": true, "port": 9090, "service": map[string]any{"enabled": true}}
	}
	return v
}

const (
	tmplShopWebServiceAccount = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .Release.Name }}
  labels:{{ .Labels | nindent 4 }}
automountServiceAccountToken: false`

	tmplShopWebSecret = `apiVersion: v1
kind: Secret
metadata:
  name: {{ .Release.Name }}-db
  labels:{{ .Labels | nindent 4 }}
type: Opaque
data:
  username: {{ .Values.database.username | b64enc }}
  password: {{ .Values.database.password | b64enc }}`

	tmplShopWebConfigMap = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-config
  labels:{{ .Labels | nindent 4 }}
data:
  LOG_LEVEL: {{ .Values.config.logLevel | quote }}
  FEATURE_NEW_CHECKOUT: {{ .Values.config.featureFlags.newCheckout | quote }}
  FEATURE_RECOMMENDATIONS: {{ .Values.config.featureFlags.recommendations | quote }}`

	tmplShopWebMetricsService = `{{- if .Values.metrics.service.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}-metrics
  labels:{{ .Labels | nindent 4 }}
spec:
  type: ClusterIP
  ports:
    - port: {{ .Values.metrics.port }}
      targetPort: metrics
      protocol: TCP
      name: metrics
  selector:{{ .Selector | nindent 4 }}
{{- end }}`

	tmplShopWebService15 = `apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}
  labels:{{ .Labels | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: http
      protocol: TCP
      name: http
{{- if .Values.metrics.enabled }}
    - port: {{ .Values.metrics.port }}
      targetPort: metrics
      protocol: TCP
      name: metrics
{{- end }}
  selector:{{ .Selector | nindent 4 }}`

	tmplShopWebDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
  labels:{{ .Labels | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  revisionHistoryLimit: 5
  selector:
    matchLabels:{{ .Selector | nindent 6 }}
  template:
    metadata:
      annotations:
        checksum/secret: {{ index .Rendered "secret.yaml" | sha256sum }}
{{- if .Values.config }}
        checksum/config: {{ index .Rendered "configmap.yaml" | sha256sum }}
{{- end }}
      labels:{{ .Labels | nindent 8 }}
    spec:
      serviceAccountName: {{ .Release.Name }}
      securityContext:{{ .Values.podSecurityContext | toYaml | nindent 8 }}
      containers:
        - name: web
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          securityContext:{{ .Values.securityContext | toYaml | nindent 12 }}
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
            - name: metrics
              containerPort: {{ .Values.metrics.port }}
              protocol: TCP
          env:
            - name: DATABASE_HOST
              value: {{ .Values.database.host | quote }}
            - name: DATABASE_PORT
              value: {{ .Values.database.port | quote }}
            - name: DATABASE_NAME
              value: {{ .Values.database.name | quote }}
            - name: DATABASE_USER
              valueFrom:
                secretKeyRef:
                  name: {{ .Release.Name }}-db
                  key: username
            - name: DATABASE_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: {{ .Release.Name }}-db
                  key: password
{{- range $name, $value := .Values.env }}
            - name: {{ $name }}
              value: {{ $value | quote }}
{{- end }}
{{- if .Values.config }}
          envFrom:
            - configMapRef:
                name: {{ .Release.Name }}-config
{{- end }}
          livenessProbe:
            httpGet:
              path: {{ .Values.probes.liveness }}
              port: http
          readinessProbe:
            httpGet:
              path: {{ .Values.probes.readiness }}
              port: http
          resources:{{ .Values.resources | toYaml | nindent 12 }}`

	tmplShopWebIngress = `{{- if .Values.ingress.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ .Release.Name }}
  labels:{{ .Labels | nindent 4 }}
spec:
  ingressClassName: {{ .Values.ingress.className }}
  rules:
{{- range .Values.ingress.hosts }}
    - host: {{ .host | quote }}
      http:
        paths:
{{- range .paths }}
          - path: {{ .path }}
            pathType: {{ .pathType }}
            backend:
              service:
                name: {{ $.Release.Name }}
                port:
                  number: {{ $.Values.service.port }}
{{- end }}
{{- end }}
{{- end }}`

	tmplShopWebNotes = `{{ .Chart.Name }} {{ .Chart.AppVersion }} runs as {{ .Release.Name }} in {{ .Release.Namespace }}.
{{- if .Values.ingress.enabled }}

Open it at:
{{- range .Values.ingress.hosts }}
  http://{{ .host }}/
{{- end }}
{{- end }}

The database credentials are in the Secret {{ .Release.Name }}-db.`
)

var (
	shopWeb140 = &demoChart{
		meta:   chartMeta("shop-web", "1.4.0", "2.3.0", "The shop's web front end"),
		values: shopWebDefaults(false),
		templates: chartFiles(
			"serviceaccount.yaml", tmplShopWebServiceAccount,
			"secret.yaml", tmplShopWebSecret,
			"metrics-service.yaml", tmplShopWebMetricsService,
			"service.yaml", tmplServiceHTTP,
			"deployment.yaml", tmplShopWebDeployment,
			"ingress.yaml", tmplShopWebIngress,
		),
		notes: parseTemplate("NOTES.txt", tmplShopWebNotes),
	}
	shopWeb150 = &demoChart{
		meta:   chartMeta("shop-web", "1.5.0", "2.4.0", "The shop's web front end"),
		values: shopWebDefaults(true),
		templates: chartFiles(
			"serviceaccount.yaml", tmplShopWebServiceAccount,
			"secret.yaml", tmplShopWebSecret,
			"configmap.yaml", tmplShopWebConfigMap,
			"service.yaml", tmplShopWebService15,
			"deployment.yaml", tmplShopWebDeployment,
			"ingress.yaml", tmplShopWebIngress,
		),
		notes: parseTemplate("NOTES.txt", tmplShopWebNotes),
	}
)

// redis: a cache in front of the shop's database, one primary without replicas.

func redisDefaults(appVersion string) map[string]any {
	return map[string]any{
		"architecture":        "replication",
		"auth":                map[string]any{"enabled": true, "password": "", "sentinel": true},
		"commonConfiguration": "appendonly yes\nsave \"\"",
		"image":               map[string]any{"repository": "redis", "tag": appVersion + "-alpine", "pullPolicy": "IfNotPresent"},
		"master": map[string]any{
			"count":     1,
			"resources": map[string]any{},
			"persistence": map[string]any{
				"enabled": true, "size": "8Gi", "accessModes": []any{"ReadWriteOnce"},
			},
			"service": map[string]any{"type": "ClusterIP", "ports": map[string]any{"redis": 6379}},
		},
		"replica": map[string]any{
			"replicaCount": 3, "resources": map[string]any{}, "persistence": map[string]any{"enabled": true, "size": "8Gi"},
		},
		"metrics":       map[string]any{"enabled": false, "port": 9121},
		"networkPolicy": map[string]any{"enabled": true, "allowExternal": true},
	}
}

const (
	tmplRedisConfigMap = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-redis-configuration
  labels:{{ .Labels | nindent 4 }}
data:
  redis.conf: |-{{ .Values.commonConfiguration | nindent 4 }}
  master.conf: |-
    dir /data
    rename-command FLUSHDB ""
    rename-command FLUSHALL ""`

	tmplRedisHeadless = `apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}-redis-headless
  labels:{{ .Labels | nindent 4 }}
spec:
  type: ClusterIP
  clusterIP: None
  ports:
    - name: tcp-redis
      port: {{ .Values.master.service.ports.redis }}
      targetPort: redis
  selector:{{ .Selector | nindent 4 }}`

	tmplRedisMasterService = `apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}-redis-master
  labels:{{ .Labels | nindent 4 }}
    app.kubernetes.io/component: master
spec:
  type: {{ .Values.master.service.type }}
  ports:
    - name: tcp-redis
      port: {{ .Values.master.service.ports.redis }}
      targetPort: redis
  selector:{{ .Selector | nindent 4 }}
    app.kubernetes.io/component: master`

	tmplRedisMaster = `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ .Release.Name }}-redis-master
  labels:{{ .Labels | nindent 4 }}
    app.kubernetes.io/component: master
spec:
  replicas: {{ .Values.master.count }}
  serviceName: {{ .Release.Name }}-redis-headless
  selector:
    matchLabels:{{ .Selector | nindent 6 }}
      app.kubernetes.io/component: master
  template:
    metadata:
      labels:{{ .Labels | nindent 8 }}
        app.kubernetes.io/component: master
      annotations:
        checksum/configmap: {{ index .Rendered "configmap.yaml" | sha256sum }}
    spec:
      containers:
        - name: redis
          image: {{ .Values.image.repository }}:{{ .Values.image.tag }}
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          command: ["redis-server", "/opt/redis/etc/redis.conf", "--include", "/opt/redis/etc/master.conf"]
          env:
            - name: ALLOW_EMPTY_PASSWORD
              value: {{ not .Values.auth.enabled | quote }}
          ports:
            - name: redis
              containerPort: 6379
          resources:{{ .Values.master.resources | toYaml | nindent 12 }}
          volumeMounts:
            - name: redis-data
              mountPath: /data
            - name: config
              mountPath: /opt/redis/etc
      volumes:
        - name: config
          configMap:
            name: {{ .Release.Name }}-redis-configuration
  volumeClaimTemplates:
    - metadata:
        name: redis-data
      spec:
        accessModes:{{ .Values.master.persistence.accessModes | toYaml | nindent 10 }}
        resources:
          requests:
            storage: {{ .Values.master.persistence.size | quote }}`

	tmplRedisNotes = `Redis {{ .Chart.AppVersion }} ({{ .Values.architecture }}) answers on port {{ .Values.master.service.ports.redis }} at

    {{ .Release.Name }}-redis-master.{{ .Release.Namespace }}.svc.cluster.local
{{- if not .Values.auth.enabled }}

Authentication is off: every pod in the cluster can connect.
{{- end }}`
)

func redisChart(version, appVersion string) *demoChart {
	return &demoChart{
		meta:   chartMeta("redis", version, appVersion, "An in-memory key-value store"),
		values: redisDefaults(appVersion),
		templates: chartFiles(
			"configmap.yaml", tmplRedisConfigMap,
			"headless-svc.yaml", tmplRedisHeadless,
			"master/service.yaml", tmplRedisMasterService,
			"master/application.yaml", tmplRedisMaster,
		),
		notes: parseTemplate("NOTES.txt", tmplRedisNotes),
	}
}

var (
	redis2061 = redisChart("20.6.1", "8.2.1")
	redis2062 = redisChart("20.6.2", "8.2.2")
)

// search-api: the product search. 0.10.0 adds a PodDisruptionBudget.

func searchAPIDefaults(v010 bool) map[string]any {
	v := map[string]any{
		"replicaCount":  1,
		"image":         map[string]any{"repository": "registry.example.com/search/api", "tag": "", "pullPolicy": "IfNotPresent"},
		"service":       map[string]any{"type": "ClusterIP", "port": 8080},
		"elasticsearch": map[string]any{"url": "http://search-es.demo-search.svc.cluster.local:9200", "index": "products"},
		"indexing":      map[string]any{"refreshInterval": "30s", "batchSize": 500},
		"resources":     map[string]any{},
	}
	if v010 {
		v["podDisruptionBudget"] = map[string]any{"minAvailable": 1}
	}
	return v
}

const (
	tmplSearchPDB = `apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ .Release.Name }}
  labels:{{ .Labels | nindent 4 }}
spec:
  minAvailable: {{ .Values.podDisruptionBudget.minAvailable }}
  selector:
    matchLabels:{{ .Selector | nindent 6 }}`

	tmplSearchConfigMap = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}
  labels:{{ .Labels | nindent 4 }}
data:
  application.yaml: |
    elasticsearch:
      url: {{ .Values.elasticsearch.url }}
      index: {{ .Values.elasticsearch.index }}
    indexing:
      refreshInterval: {{ .Values.indexing.refreshInterval }}
      batchSize: {{ .Values.indexing.batchSize }}`

	tmplSearchDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
  labels:{{ .Labels | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:{{ .Selector | nindent 6 }}
  template:
    metadata:
      annotations:
        checksum/config: {{ index .Rendered "configmap.yaml" | sha256sum }}
      labels:{{ .Labels | nindent 8 }}
    spec:
      containers:
        - name: api
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          args: ["--config", "/etc/search-api/application.yaml"]
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
          readinessProbe:
            httpGet:
              path: /health/ready
              port: http
          resources:{{ .Values.resources | toYaml | nindent 12 }}
          volumeMounts:
            - name: config
              mountPath: /etc/search-api
      volumes:
        - name: config
          configMap:
            name: {{ .Release.Name }}`

	tmplSearchNotes = `{{ .Release.Name }} {{ .Chart.AppVersion }} answers at http://{{ .Release.Name }}.{{ .Release.Namespace }}.svc.cluster.local:{{ .Values.service.port }}/`
)

var (
	searchAPI090 = &demoChart{
		meta:   chartMeta("search-api", "0.9.0", "1.8.0", "The product search API"),
		values: searchAPIDefaults(false),
		templates: chartFiles(
			"configmap.yaml", tmplSearchConfigMap,
			"service.yaml", tmplServiceHTTP,
			"deployment.yaml", tmplSearchDeployment,
		),
		notes: parseTemplate("NOTES.txt", tmplSearchNotes),
	}
	searchAPI0100 = &demoChart{
		meta:   chartMeta("search-api", "0.10.0", "1.9.0", "The product search API"),
		values: searchAPIDefaults(true),
		templates: chartFiles(
			"pdb.yaml", tmplSearchPDB,
			"configmap.yaml", tmplSearchConfigMap,
			"service.yaml", tmplServiceHTTP,
			"deployment.yaml", tmplSearchDeployment,
		),
		notes: parseTemplate("NOTES.txt", tmplSearchNotes),
	}
)
