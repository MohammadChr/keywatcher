# Agent: Infrastructure

You own the Dockerfile and Helm chart. Read CLAUDE.md first.
Target: Kubernetes. Single binary. Distroless image under 20MB.

## Task 1 — Dockerfile (deploy/docker/Dockerfile)
```dockerfile
# ── Stage 1: Build ──────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /vaultwatch ./main.go

# ── Stage 2: Runtime ────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /vaultwatch /vaultwatch

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/vaultwatch"]
```

## Task 2 — Helm Chart

Create deploy/helm/Chart.yaml:
```yaml
apiVersion: v2
name: vaultwatch
description: DevOps asset expiry manager
type: application
version: 0.1.0
appVersion: "0.1.0"
```

Create deploy/helm/values.yaml:
```yaml
replicaCount: 2

image:
  repository: your-registry/vaultwatch
  pullPolicy: IfNotPresent
  tag: "latest"

service:
  type: ClusterIP
  port: 8080

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    cpu: 200m
    memory: 128Mi

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70

serviceMonitor:
  enabled: false   # set true if Prometheus Operator is installed

ingress:
  enabled: false
  className: ""
  host: vaultwatch.example.com
  tls: false

config:
  logLevel: info
  authMethods: "local"
  jwtSecretName: vaultwatch-jwt          # k8s secret name
  jwtSecretKey: jwt-secret               # key inside the secret

  expiry:
    checkInterval: "1h"
    warnDays: "30,14,7,1"

  notifications:
    slackWebhookSecretName: ""           # k8s secret name (leave empty to disable)
    mattermostWebhookSecretName: ""
    genericWebhookSecretName: ""

postgresql:
  host: postgres
  port: 5432
  database: vaultwatch
  sslMode: require
  secretName: vaultwatch-db             # k8s secret name
  secretKey: database-url               # key inside the secret holding full DSN

oidc:
  enabled: false
  issuer: ""
  clientID: ""
  clientSecretName: ""
  clientSecretKey: ""

ldap:
  enabled: false
  url: ""
  bindDN: ""
  baseDN: ""
  userFilter: "(uid=%s)"
  bindPasswordSecretName: ""
  bindPasswordSecretKey: ""
```

Create deploy/helm/templates/deployment.yaml:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "vaultwatch.fullname" . }}
  labels:
    {{- include "vaultwatch.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "vaultwatch.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
      labels:
        {{- include "vaultwatch.selectorLabels" . | nindent 8 }}
    spec:
      serviceAccountName: {{ include "vaultwatch.fullname" . }}
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
      containers:
        - name: vaultwatch
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - containerPort: 8080
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 15
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          env:
            - name: VAULTWATCH_PORT
              value: "8080"
            - name: VAULTWATCH_LOG_LEVEL
              value: {{ .Values.config.logLevel | quote }}
            - name: VAULTWATCH_AUTH_METHODS
              value: {{ .Values.config.authMethods | quote }}
            - name: VAULTWATCH_CHECK_INTERVAL
              value: {{ .Values.config.expiry.checkInterval | quote }}
            - name: VAULTWATCH_WARN_DAYS
              value: {{ .Values.config.expiry.warnDays | quote }}
            - name: VAULTWATCH_JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.config.jwtSecretName }}
                  key: {{ .Values.config.jwtSecretKey }}
            - name: VAULTWATCH_DB_URL
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.postgresql.secretName }}
                  key: {{ .Values.postgresql.secretKey }}
            {{- if .Values.oidc.enabled }}
            - name: VAULTWATCH_OIDC_ISSUER
              value: {{ .Values.oidc.issuer | quote }}
            - name: VAULTWATCH_OIDC_CLIENT_ID
              value: {{ .Values.oidc.clientID | quote }}
            - name: VAULTWATCH_OIDC_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.oidc.clientSecretName }}
                  key: {{ .Values.oidc.clientSecretKey }}
            {{- end }}
            {{- if .Values.ldap.enabled }}
            - name: VAULTWATCH_LDAP_URL
              value: {{ .Values.ldap.url | quote }}
            - name: VAULTWATCH_LDAP_BIND_DN
              value: {{ .Values.ldap.bindDN | quote }}
            - name: VAULTWATCH_LDAP_BASE_DN
              value: {{ .Values.ldap.baseDN | quote }}
            - name: VAULTWATCH_LDAP_USER_FILTER
              value: {{ .Values.ldap.userFilter | quote }}
            - name: VAULTWATCH_LDAP_BIND_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.ldap.bindPasswordSecretName }}
                  key: {{ .Values.ldap.bindPasswordSecretKey }}
            {{- end }}
```

Create deploy/helm/templates/_helpers.tpl:
```
{{- define "vaultwatch.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "vaultwatch.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{ include "vaultwatch.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "vaultwatch.selectorLabels" -}}
app.kubernetes.io/name: vaultwatch
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
```

Create deploy/helm/templates/service.yaml:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "vaultwatch.fullname" . }}
  labels:
    {{- include "vaultwatch.labels" . | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: 8080
      protocol: TCP
      name: http
  selector:
    {{- include "vaultwatch.selectorLabels" . | nindent 4 }}
```

Create deploy/helm/templates/serviceaccount.yaml:
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "vaultwatch.fullname" . }}
  labels:
    {{- include "vaultwatch.labels" . | nindent 4 }}
```

Create deploy/helm/templates/hpa.yaml:
```yaml
{{- if .Values.autoscaling.enabled }}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ include "vaultwatch.fullname" . }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ include "vaultwatch.fullname" . }}
  minReplicas: {{ .Values.autoscaling.minReplicas }}
  maxReplicas: {{ .Values.autoscaling.maxReplicas }}
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ .Values.autoscaling.targetCPUUtilizationPercentage }}
{{- end }}
```

Create deploy/helm/templates/servicemonitor.yaml:
```yaml
{{- if .Values.serviceMonitor.enabled }}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "vaultwatch.fullname" . }}
  labels:
    {{- include "vaultwatch.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "vaultwatch.selectorLabels" . | nindent 6 }}
  endpoints:
    - port: http
      path: /metrics
      interval: 30s
{{- end }}
```

## Final check
Run `helm lint deploy/helm/` — fix any warnings before finishing.
