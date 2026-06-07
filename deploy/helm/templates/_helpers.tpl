{{- define "keywatcher.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "keywatcher.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{ include "keywatcher.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "keywatcher.selectorLabels" -}}
app.kubernetes.io/name: keywatcher
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
