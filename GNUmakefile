OS_ARCH     := $(shell go env GOOS)_$(shell go env GOARCH)
DEV_VERSION := 99.0.0
PLUGIN_DIR  := $(HOME)/.terraform.d/plugins/registry.terraform.io/laurentlesle/rest/$(DEV_VERSION)/$(OS_ARCH)
BINARY      := terraform-provider-rest

.PHONY: build dev-install clean

## build: compile the provider binary
build:
	go build -o $(BINARY) .

## dev-install: build and install into the local filesystem mirror for testing
dev-install: build
	@mkdir -p $(PLUGIN_DIR)
	cp $(BINARY) $(PLUGIN_DIR)/$(BINARY)_v$(DEV_VERSION)
	@echo ""
	@echo "Installed $(BINARY)_v$(DEV_VERSION) → $(PLUGIN_DIR)"
	@echo ""
	@echo "Use in Terraform:"
	@echo "  required_providers {"
	@echo "    rest = { source = \"laurentlesle/rest\", version = \"$(DEV_VERSION)\" }"
	@echo "  }"

## clean: remove the compiled binary
clean:
	rm -f $(BINARY)
