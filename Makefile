# ---------- CONFIG ----------
PROTO_DIR=proto
PROTO_FILES=$(wildcard $(PROTO_DIR)/*.proto)

# ---------- DEFAULT ----------
.PHONY: help
help:
	@echo "Available commands:"
	@echo "  make setup-master            - install dev tools for the master/load balancer"
	@echo "  make generate-proto          - generate all code (proto etc.)"
	@echo "  make generate-proto-go       - generate Go code from proto"
	@echo "  make generate-proto-python   - generate Python code from proto"
	@echo "  !! please check the Makefile because a lot of commands are not here" 

# ---------- SETUP ----------
.PHONY: setup-master
install-master:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

install-worker:
	cd worker && pip install -r requirements.txt

# ---------- CODEGEN ----------
.PHONY: generate-proto generate-proto-go generate-proto-python

generate-proto: generate-proto-go generate-proto-python

generate-proto-go: install-master
	mkdir -p master/generated 
	protoc --go_out=. --go-grpc_out=. $(PROTO_FILES)

generate-proto-python: install-worker
	mkdir -p worker/generated
	touch worker/generated/__init__.py
	python -m grpc_tools.protoc -I proto --python_out=worker/ --grpc_python_out=worker/ proto/worker.proto

# ---------- CLEAN ----------
.PHONY: clean
clean:
	@echo "The clean target is not implemented yet"