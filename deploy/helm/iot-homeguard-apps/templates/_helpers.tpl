{{/*
Expand the name of the chart.
*/}}
{{- define "iot-homeguard-apps.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a fully qualified app name.
*/}}
{{- define "iot-homeguard-apps.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "iot-homeguard-apps.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "iot-homeguard-apps.labels" -}}
helm.sh/chart: {{ include "iot-homeguard-apps.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: iot-homeguard
{{- end }}

{{/*
Selector labels for a specific component
*/}}
{{- define "iot-homeguard-apps.selectorLabels" -}}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Build image name with registry
*/}}
{{- define "iot-homeguard-apps.image" -}}
{{- $registry := .Values.imageRegistry.registry -}}
{{- $prefix := .Values.imageRegistry.prefix -}}
{{- $image := .image -}}
{{- $tag := .tag -}}
{{- if $registry -}}
{{- printf "%s/%s/%s:%s" $registry $prefix $image $tag -}}
{{- else -}}
{{- printf "%s/%s:%s" $prefix $image $tag -}}
{{- end -}}
{{- end }}

{{/*
PostgreSQL URL
*/}}
{{- define "iot-homeguard-apps.postgresUrl" -}}
{{- $host := .Values.global.infrastructure.postgresql.host -}}
{{- $port := .Values.global.infrastructure.postgresql.port -}}
{{- $db := .Values.global.infrastructure.postgresql.database -}}
{{- $user := .Values.global.infrastructure.postgresql.username -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "postgresql://%s:$(POSTGRES_PASSWORD)@%s.%s:%d/%s?sslmode=disable" $user $host $ns (int $port) $db -}}
{{- end }}

{{/*
MongoDB URL
*/}}
{{- define "iot-homeguard-apps.mongoUrl" -}}
{{- $host := .Values.global.infrastructure.mongodb.host -}}
{{- $port := .Values.global.infrastructure.mongodb.port -}}
{{- $db := .Values.global.infrastructure.mongodb.database -}}
{{- $user := .Values.global.infrastructure.mongodb.username -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "mongodb://%s:$(MONGO_PASSWORD)@%s.%s:%d/%s?authSource=admin" $user $host $ns (int $port) $db -}}
{{- end }}

{{/*
Redis URL
*/}}
{{- define "iot-homeguard-apps.redisUrl" -}}
{{- $host := .Values.global.infrastructure.redis.host -}}
{{- $port := .Values.global.infrastructure.redis.port -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "redis://%s.%s:%d" $host $ns (int $port) -}}
{{- end }}

{{/*
Kafka Brokers
*/}}
{{- define "iot-homeguard-apps.kafkaBrokers" -}}
{{- $host := .Values.global.infrastructure.kafka.host -}}
{{- $port := .Values.global.infrastructure.kafka.port -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "%s.%s:%d" $host $ns (int $port) -}}
{{- end }}

{{/*
TimescaleDB URL
*/}}
{{- define "iot-homeguard-apps.timescaleUrl" -}}
{{- $host := .Values.global.infrastructure.timescaledb.host -}}
{{- $port := .Values.global.infrastructure.timescaledb.port -}}
{{- $db := .Values.global.infrastructure.timescaledb.database -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "postgres://homeguard:$(POSTGRES_PASSWORD)@%s.%s:%d/%s?sslmode=disable" $host $ns (int $port) $db -}}
{{- end }}

{{/*
ScyllaDB Hosts
*/}}
{{- define "iot-homeguard-apps.scyllaHosts" -}}
{{- $host := .Values.global.infrastructure.scylladb.host -}}
{{- $port := .Values.global.infrastructure.scylladb.port -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "%s.%s:%d" $host $ns (int $port) -}}
{{- end }}

{{/*
Service URL builder
*/}}
{{- define "iot-homeguard-apps.serviceUrl" -}}
{{- $prefix := .Values.global.namePrefix -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "http://%s-%s.%s:8080" $prefix .service $ns -}}
{{- end }}
