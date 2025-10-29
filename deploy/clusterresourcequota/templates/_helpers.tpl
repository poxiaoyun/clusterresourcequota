{{/*
Return the proper image name
{{ include "common.images.image" ( dict "imageRoot" .Values.path.to.the.image "global" $) }}
*/}}

{{- define "clusterresourcequota.fullname" -}}
{{ template "common.names.fullname" . }}
{{- end -}}

{{/*
Return the proper image name
*/}}
{{- define "clusterresourcequota.image" -}}
{{ include "common.images.image" (dict "imageRoot" .Values.clusterresourcequota.image "root" . "global" .Values.global) }}
{{- end -}}

{{/*
Return the proper Docker Image Registry Secret Names
*/}}
{{- define "clusterresourcequota.imagePullSecrets" -}}
{{- include "common.images.renderPullSecrets" (dict "images" (list .Values.clusterresourcequota.image) "context" $) -}}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "clusterresourcequota.serviceAccountName" -}}
{{ default (include "clusterresourcequota.fullname" .) .Values.clusterresourcequota.serviceAccount.name }}
{{- end -}}

{{- define "clusterresourcequota.webhook.secretName" -}}
{{- if .Values.admissionWebhooks.secretName -}}
    {{- .Values.admissionWebhooks.secretName -}}
{{- else }}
    {{- include "clusterresourcequota.fullname" . -}}
{{- end -}}
{{- end -}}
