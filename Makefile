NAME=pr-review
VERSION=0.0.1
REGISTRY_PREFIX=172.24.173.77:30500/

# .PHONY: build publish version

build:
	docker build -t ${NAME}:${VERSION} .

publish:
	docker tag ${NAME}:${VERSION} ${REGISTRY_PREFIX}${NAME}:${VERSION}
	docker push ${REGISTRY_PREFIX}${NAME}:${VERSION}

version:
	@echo ${VERSION}
