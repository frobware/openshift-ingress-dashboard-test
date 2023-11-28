# If this Makefile changes, then everthing should be rebuilt.
.EXTRA_PREREQS := $(abspath $(lastword $(MAKEFILE_LIST)))

REGISTRY ?= quay.io
IMAGE    ?= amcdermo/openshift-ingress-operator-ocpstrat139

.PHONY: all client server clean image push-image

all: client server

client:
	CGO_ENABLED=0 go build -o bin/client ./client/...

server:
	CGO_ENABLED=0 go build -o bin/server ./server/...

clean:
	$(RM) -f bin/client bin/server

image: client server
	podman build -t localhost/$(IMAGE) .
	podman tag localhost/$(IMAGE):latest $(REGISTRY)/$(IMAGE)

push-image: image
	podman push $(REGISTRY)/$(IMAGE)

deploy-manifests:
	oc delete --ignore-not-found -f ./manifests
	oc apply -f ./manifests
