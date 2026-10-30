{{/*
Expand the name of the chart.
*/}}
{{- define "cloudpam-server.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "cloudpam-server.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "cloudpam-server.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "cloudpam-server.labels" -}}
helm.sh/chart: {{ include "cloudpam-server.chart" . }}
{{ include "cloudpam-server.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "cloudpam-server.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cloudpam-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "cloudpam-server.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "cloudpam-server.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Get the secret name
*/}}
{{- define "cloudpam-server.secretName" -}}
{{- if .Values.existingSecret }}
{{- .Values.existingSecret }}
{{- else }}
{{- include "cloudpam-server.fullname" . }}
{{- end }}
{{- end }}

{{/* Secret-looking names stay out of the ConfigMap without a chart change. */}}
{{- define "cloudpam-server.secretNamePattern" -}}
^(DATABASE_URL|SENTRY_DSN)$|_(PASSWORD|SECRET|TOKEN|KEY)$
{{- end }}

{{- define "cloudpam-server.envString" -}}
{{- if kindIs "slice" . }}{{ join "," . }}{{ else if not (kindIs "invalid" .) }}{{ toString . }}{{ end }}
{{- end }}

{{- define "cloudpam-server.configData" -}}
{{- $env := dict "APP_VERSION" (.Values.image.tag | default .Chart.AppVersion | toString) -}}
{{- if eq .Values.storage.backend "sqlite" -}}
{{- $_ := set $env "SQLITE_DSN" .Values.storage.sqlite.dsn -}}
{{- end -}}
{{- range $name, $value := .Values.config -}}
{{- if has $name (list "ADDR" "PORT" "APP_VERSION" "SQLITE_DSN") -}}
{{- fail (printf "config.%s is set by the chart (image.tag, storage.sqlite.dsn, the fixed port 8080)" $name) -}}
{{- end -}}
{{- if regexMatch (include "cloudpam-server.secretNamePattern" $) $name -}}
{{- fail (printf "config.%s looks like a credential; set secrets.%s instead" $name $name) -}}
{{- end -}}
{{- $s := include "cloudpam-server.envString" $value -}}
{{- if $s }}{{ $_ := set $env $name $s }}{{ end -}}
{{- end -}}
{{- toYaml $env -}}
{{- end }}

{{- define "cloudpam-server.secretData" -}}
{{- $data := dict -}}
{{- range $name, $value := .Values.secrets -}}
{{- $s := include "cloudpam-server.envString" $value -}}
{{- if $s }}{{ $_ := set $data $name $s }}{{ end -}}
{{- end -}}
{{- toYaml $data -}}
{{- end }}

{{- define "cloudpam-server.databaseUrlFromEnv" -}}
{{- range .Values.env }}
{{- if eq .name "DATABASE_URL" }}
{{- if not (or .value .valueFrom) }}
{{- fail "env entry DATABASE_URL has no value or valueFrom; an empty entry would override the Secret and the server would fall back to SQLite" }}
{{- end }}true
{{- end }}
{{- end }}
{{- end }}

{{- define "cloudpam-server.validateStorage" -}}
{{- $backend := .Values.storage.backend -}}
{{- if not (has $backend (list "sqlite" "postgres")) -}}
{{- fail (printf "storage.backend must be sqlite or postgres, got %q" $backend) -}}
{{- end -}}
{{- $dbInline := include "cloudpam-server.envString" (dig "DATABASE_URL" "" (.Values.secrets | default dict)) -}}
{{- $dbEnv := include "cloudpam-server.databaseUrlFromEnv" . -}}
{{- if eq $backend "sqlite" -}}
{{- if gt (int .Values.replicaCount) 1 -}}
{{- fail "storage.backend=sqlite runs one replica; set replicaCount=1 or storage.backend=postgres" -}}
{{- end -}}
{{- if or $dbInline $dbEnv -}}
{{- fail "DATABASE_URL is set, so the server would use Postgres; set storage.backend=postgres" -}}
{{- end -}}
{{- else if not (or .Values.existingSecret $dbInline $dbEnv) -}}
{{- fail "storage.backend=postgres needs DATABASE_URL in secrets, existingSecret or env" -}}
{{- end -}}
{{- if .Values.existingSecret -}}
{{- range $name, $value := .Values.secrets -}}
{{- if include "cloudpam-server.envString" $value -}}
{{- fail (printf "secrets.%s is ignored when existingSecret is set; add %s to Secret %s" $name $name $.Values.existingSecret) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end }}
