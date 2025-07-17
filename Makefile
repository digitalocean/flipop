GO_VERSION := 1.23.1
COMMIT ?= $(shell git rev-parse --short HEAD)
SHELL=/bin/bash -o pipefail

image: dev-image
	docker build --build-arg=FLIPOP_VERSION=$(COMMIT) -t digitaloceanapps/flipop:$(COMMIT) .
ifdef latest
	docker tag digitaloceanapps/flipop:$(COMMIT) digitaloceanapps/flipop:latest
endif

# dev-image is the builder image for the production image, and also provides everything necessary
# for local development.
dev-image:
	docker build --no-cache -t flipop-dev -f Dockerfile.dev \
		--build-arg=GO_IMAGE=golang:$(GO_VERSION) .

generate-k8s:
	docker run -v $$(pwd):/go/src/github.com/digitalocean/flipop/ \
		flipop-dev:latest \
		/go/src/k8s.io/code-generator/generate-groups.sh \
		all \
		github.com/digitalocean/flipop/pkg/apis/flipop/generated \
		github.com/digitalocean/flipop/pkg/apis \
		flipop:v1alpha1 \
		--go-header-file=hack/boilerplate.go.txt

image-push: image
	docker push digitaloceanapps/flipop:$(COMMIT)
ifdef latest
	docker push digitaloceanapps/flipop:latest
endif

test: dev-image
	docker run flipop-dev go test github.com/digitalocean/flipop/...

.PHONY: dev image
