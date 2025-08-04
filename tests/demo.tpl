这个是template内容
${MONGO_DB:dbname}
{{- define "demo" -}}
{{- $name := .name -}}
{{- $age := .age -}}
{{- $hobby := .hobby -}}
{{- $hobbyStr := "" -}}
{{- range $index, $item := $hobby -}}
{{- if eq $index 0 -}}
{{$hobbyStr = $item}}
{{- else -}}
{{$hobbyStr = printf "%s,%s" $hobbyStr $item}}
{{- end -}}
{{- end -}}