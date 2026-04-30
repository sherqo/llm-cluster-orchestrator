# ---------- CONFIG ----------
PROTO_DIR=proto
PROTO_FILES=$(wildcard $(PROTO_DIR)/*.proto)

# ---------- DEFAULT ----------
.PHONY: help
help:
	@echo "Available commands:"
	@echo "  make setup-master 						- install dev tools for the master/load balancer"
	@echo "  make generate-proto     			- generate all code (proto etc.)"
	@echo "  make generate-proto-go       - generate Go code from proto"
	@echo "  make generate-proto-python   - generate Python code from proto"

# ---------- SETUP ----------
.PHONY: setup-master
setup-master:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# ---------- CODEGEN ----------
.PHONY: generate-proto generate-proto-go generate-proto-python

generate-proto: generate-proto-go generate-proto-python

generate-proto-go:
# 	mkdir -p master/generated 
	protoc --go_out=. --go-grpc_out=. $(PROTO_FILES)

generate-proto-python:
	@echo "Python proto generation not implemented yet"

# ---------- CLEAN ----------
.PHONY: clean
clean:
	@echo "The clean target is not implemented yet"