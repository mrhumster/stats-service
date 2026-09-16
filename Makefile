.PHONY: build push deploy clean test

IMAGE_NAME := xomrkob/stats-service
VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
	docker build --build-arg VERSION=$(VERSION) --build-arg BUILD_DATE=$(BUILD_DATE) -t $(IMAGE_NAME) -t $(IMAGE_NAME):$(VERSION) -f Dockerfile ..

push:
	docker push $(IMAGE_NAME):$(VERSION)
	docker push $(IMAGE_NAME):latest

deploy:
	kubectl set image deployment/stats-reader stats-reader=$(IMAGE_NAME):$(VERSION) -n go-app
	kubectl rollout status deployment/stats-reader -n go-app --timeout=180s

clean:
	rm -f stats-server

test:
	go test ./...