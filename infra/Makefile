.PHONY: build push ecr-login binary

IMAGE_NAME ?= my-dev/gobore-server
IMAGE_TAG ?= latest
REGISTRY ?= 144465910773.dkr.ecr.us-west-2.amazonaws.com
AWS_REGION ?= us-west-2

binary:
	cd .. && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o infra/gobore ./cmd/bore

build: binary
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

ecr-login:
	aws ecr get-login-password --region $(AWS_REGION) | docker login --username AWS --password-stdin $(REGISTRY)

push: build ecr-login
	docker tag $(IMAGE_NAME):$(IMAGE_TAG) $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
