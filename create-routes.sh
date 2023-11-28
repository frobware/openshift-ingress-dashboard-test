#!/usr/bin/env bash

if [ $# -lt 1 ]; then
    echo "usage: ${0##/*} <file>" >&2
    exit 1
fi

for i in $(yq e '.errorPlans[].errorCode' "$1"); do
    echo "---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: ocpstrat139-$i
spec:
  to:
    kind: Service
    name: ocpstrat139
    weight: 100
  port:
    targetPort: http"
done
