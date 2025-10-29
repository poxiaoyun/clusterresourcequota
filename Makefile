
BUILD_DATE?=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GIT_VERSION?=$(shell git describe --tags --dirty --abbrev=0 2>/dev/null || git symbolic-ref --short HEAD)
GIT_COMMIT?=$(shell git rev-parse HEAD 2>/dev/null)
GIT_BRANCH?=$(shell git symbolic-ref --short HEAD 2>/dev/null)
VERSION?=$(shell echo "${GIT_VERSION}" | sed -e 's/^v//')

BIN_DIR?=bin
IMAGE_REGISTRY?=registry.cn-hangzhou.aliyuncs.com
IMAGE_REPOSITORY?=xiaoshiai
IMAGE_NAME?=clusterresourcequota

# oci registry username and password
REGISTRY_USERNAME?=
REGISTRY_PASSWORD?=

LDFLAGS+=-w -s
LDFLAGS+=-X 'xiaoshiai.cn/common/version.gitVersion=${GIT_VERSION}'
LDFLAGS+=-X 'xiaoshiai.cn/common/version.gitCommit=${GIT_COMMIT}'
LDFLAGS+=-X 'xiaoshiai.cn/common/version.buildDate=${BUILD_DATE}'

.PHONY: all
all: build

generate: generate-code generate-crd ## Generate code for the project.

generate-crd:
	@echo "Generating CRD manifests..."
	$(CONTROLLER_GEN) paths="./apis/..." crd  output:crd:artifacts:config=deploy/clusterresourcequota/crds

generate-code:
	@echo "Generating clientset..."
	@./hack/update-codegen.sh

generate-certs:
	@echo "Generating TLS certificates..."
	@./hack/generate-certs.sh

define build-binary
	@echo "Building ${1}-${2}";
	@mkdir -p ${BIN_DIR}/${1}-${2};
	GOOS=${1} GOARCH=$(2) CGO_ENABLED=0 go build -gcflags=all="-N -l" -ldflags="${LDFLAGS}" -o ${BIN_DIR}/${1}-${2} ./cmd/...
endef

build: build-binary

.PHONY: build-binary
build-binary:
	$(call build-binary,linux,amd64)
	$(call build-binary,linux,arm64)

build-helm:
	helm dependency build charts/clusterresourcequota
	helm package charts/clusterresourcequota --version=${VERSION} --app-version=${VERSION} --destination ${BIN_DIR}

.PHONY: release
release:release-image release-helm

BUILDX_PLATFORMS?=linux/amd64,linux/arm64
release-image:
	docker buildx build --platform=${BUILDX_PLATFORMS} --push -t $(IMAGE_REGISTRY)/$(IMAGE_REPOSITORY)/$(IMAGE_NAME):$(GIT_VERSION) -f Dockerfile ${BIN_DIR}

release-helm: build-helm
	helm push ${BIN_DIR}/clusterresourcequota-${VERSION}.tgz oci://${IMAGE_REPOSITORY}

login:
	docker login ${IMAGE_REGISTRY} -u ${REGISTRY_USERNAME} -p ${REGISTRY_PASSWORD}
	helm registry login ${IMAGE_REGISTRY} -u ${REGISTRY_USERNAME} -p ${REGISTRY_PASSWORD}

CONTROLLER_GEN = ${BIN_DIR}/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	GOBIN=$(abspath ${BIN_DIR}) go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0

clean:
	rm -rf ${BIN_DIR}
