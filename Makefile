MODULES := $(patsubst $(CURDIR)/%,./%,$(shell go list -m -f '{{.Dir}}'))
APPS    := $(filter ./apps/%,$(MODULES))
LIBS    := $(filter-out ./apps/%,$(MODULES))

.PHONY: test build build-apps build-libs test-apps test-libs

test: test-libs test-apps

build: build-libs build-apps

build-apps:
	@for mod in $(APPS); do \
		echo "=== building $$mod ==="; \
		(cd $$mod && go build ./...) || exit 1; \
	done

build-libs:
	@for mod in $(LIBS); do \
		echo "=== building $$mod ==="; \
		(cd $$mod && go build ./...) || exit 1; \
	done

test-apps:
	@for mod in $(APPS); do \
		echo "=== testing $$mod ==="; \
		(cd $$mod && go test ./...) || exit 1; \
	done

test-libs:
	@for mod in $(LIBS); do \
		echo "=== testing $$mod ==="; \
		(cd $$mod && go test ./...) || exit 1; \
	done
