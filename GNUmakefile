OS_ARCH     := $(shell go env GOOS)_$(shell go env GOARCH)
DEV_VERSION := 99.0.0
PLUGIN_DIR  := $(HOME)/.terraform.d/plugins/registry.terraform.io/laurentlesle/rest/$(DEV_VERSION)/$(OS_ARCH)
BINARY      := terraform-provider-rest
BRANCH      ?=

.PHONY: build dev-install clean

WORKTREE_BASE := $(HOME)/.dev-worktrees

## build: compile the provider binary (from current branch, or BRANCH= if set)
build:
ifdef BRANCH
	@echo "→ Building from branch: $(BRANCH)"
	@git fetch origin $(BRANCH) 2>/dev/null; true
	@WORKTREE="$(WORKTREE_BASE)/terraform-provider-rest-$(BRANCH)" && \
	  mkdir -p "$(WORKTREE_BASE)" && \
	  git worktree remove --force "$$WORKTREE" 2>/dev/null; true && \
	  git worktree add --detach "$$WORKTREE" "$(BRANCH)" && \
	  (cd "$$WORKTREE" && go build -o "$(CURDIR)/$(BINARY)" .) && \
	  git worktree remove --force "$$WORKTREE"
else
	go build -o $(BINARY) .
endif

## dev-install: build and install into the local filesystem mirror (use BRANCH= to target a specific branch)
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
