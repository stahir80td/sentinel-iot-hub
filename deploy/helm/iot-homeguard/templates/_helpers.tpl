{{/*
Expand the name of the chart with prefix.
*/}}
{{- define "iot-homeguard.name" -}}
{{- printf "%s-%s" .Values.global.namePrefix .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a prefixed name for a component.
*/}}
{{- define "iot-homeguard.componentName" -}}
{{- $prefix := .Values.global.namePrefix -}}
{{- $name := .name -}}
{{- printf "%s-%s" $prefix $name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "iot-homeguard.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: iot-homeguard
{{- end }}

{{/*
Selector labels for a component
*/}}
{{- define "iot-homeguard.selectorLabels" -}}
app.kubernetes.io/name: {{ .name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the namespace
*/}}
{{- define "iot-homeguard.namespace" -}}
{{- .Values.global.namespace }}
{{- end }}

{{/*
PostgreSQL connection string
*/}}
{{- define "iot-homeguard.postgresqlUrl" -}}
{{- $prefix := .Values.global.namePrefix -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "postgresql://%s:%s@%s-postgresql.%s:5432/%s?sslmode=disable" .Values.postgresql.auth.username .Values.postgresql.auth.password $prefix $ns .Values.postgresql.auth.database }}
{{- end }}

{{/*
MongoDB connection string
*/}}
{{- define "iot-homeguard.mongodbUri" -}}
{{- $prefix := .Values.global.namePrefix -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "mongodb://%s:%s@%s-mongodb.%s:27017/%s?authSource=admin" .Values.mongodb.auth.rootUsername .Values.mongodb.auth.rootPassword $prefix $ns .Values.mongodb.auth.database }}
{{- end }}

{{/*
Redis URL
*/}}
{{- define "iot-homeguard.redisUrl" -}}
{{- $prefix := .Values.global.namePrefix -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "redis://%s-redis.%s:6379" $prefix $ns }}
{{- end }}

{{/*
TimescaleDB connection string
*/}}
{{- define "iot-homeguard.timescaledbUrl" -}}
{{- $prefix := .Values.global.namePrefix -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "postgresql://%s:%s@%s-timescaledb.%s:5432/%s?sslmode=disable" .Values.timescaledb.auth.username .Values.timescaledb.auth.password $prefix $ns .Values.timescaledb.auth.database }}
{{- end }}

{{/*
ScyllaDB hosts
*/}}
{{- define "iot-homeguard.scylladbHosts" -}}
{{- $prefix := .Values.global.namePrefix -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "%s-scylladb.%s:9042" $prefix $ns }}
{{- end }}

{{/*
Kafka bootstrap servers
*/}}
{{- define "iot-homeguard.kafkaBrokers" -}}
{{- $prefix := .Values.global.namePrefix -}}
{{- $ns := .Values.global.namespace -}}
{{- printf "%s-kafka.%s:9092" $prefix $ns }}
{{- end }}

{{/*
Service URL helper
*/}}
{{- define "iot-homeguard.serviceUrl" -}}
{{- $prefix := .Values.global.namePrefix -}}
{{- $ns := .Values.global.namespace -}}
{{- $name := .name -}}
{{- $port := .port | default 8080 -}}
{{- printf "http://%s-%s.%s:%d" $prefix $name $ns (int $port) }}
{{- end }}
