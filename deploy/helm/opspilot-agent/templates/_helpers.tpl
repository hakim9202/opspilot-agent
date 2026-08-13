{{- define "opspilot-agent.name" -}}
opspilot-agent
{{- end -}}

{{- define "opspilot-agent.fullname" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opspilot-agent.labels" -}}
app.kubernetes.io/name: {{ include "opspilot-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "opspilot-agent.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "opspilot-agent.name" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
