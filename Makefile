SHELL := /bin/sh

BIN_DIR := $(CURDIR)/.bin
RUN_DIR := $(CURDIR)/.run
LOG_DIR := $(CURDIR)/.logs

DNS_FORWARDER_BIN := $(BIN_DIR)/dns-forwarder
VERCEL_UPDATER_BIN := $(BIN_DIR)/vercel-updater
DNS_FORWARDER_PID := $(RUN_DIR)/dns-forwarder.pid
VERCEL_UPDATER_PID := $(RUN_DIR)/vercel-updater.pid
DNS_FORWARDER_LOG := $(LOG_DIR)/dns-forwarder.log
VERCEL_UPDATER_LOG := $(LOG_DIR)/vercel-updater.log
DNS_FORWARDER_SOURCES := $(shell find dns-forwarder cmd/dns-forwarder -type f -name '*.go')
VERCEL_UPDATER_SOURCES := $(shell find vercel-updater cmd/vercel-updater -type f -name '*.go')

.PHONY: build start stop restart \
	start-dns-forwarder start-vercel-updater \
	stop-dns-forwarder stop-vercel-updater \
	grant-dns-bind

build: $(DNS_FORWARDER_BIN) $(VERCEL_UPDATER_BIN)

$(DNS_FORWARDER_BIN): go.mod go.sum $(DNS_FORWARDER_SOURCES)
	@mkdir -p "$(BIN_DIR)"
	go build -o "$(DNS_FORWARDER_BIN)" ./cmd/dns-forwarder

$(VERCEL_UPDATER_BIN): go.mod go.sum $(VERCEL_UPDATER_SOURCES)
	@mkdir -p "$(BIN_DIR)"
	go build -o "$(VERCEL_UPDATER_BIN)" ./cmd/vercel-updater

start: start-dns-forwarder start-vercel-updater

start-dns-forwarder: $(DNS_FORWARDER_BIN)
	@mkdir -p "$(RUN_DIR)" "$(LOG_DIR)"
	@if [ -f "$(DNS_FORWARDER_PID)" ]; then \
		pid=$$(cat "$(DNS_FORWARDER_PID)"); \
		if kill -0 "$$pid" 2>/dev/null && ps -p "$$pid" -o args= | grep -Fq "$(DNS_FORWARDER_BIN)"; then \
			echo "dns-forwarder is already running (PID $$pid)"; \
			exit 0; \
		fi; \
		rm -f "$(DNS_FORWARDER_PID)"; \
	fi; \
	cd "$(CURDIR)"; \
	nohup "$(DNS_FORWARDER_BIN)" >>"$(DNS_FORWARDER_LOG)" 2>&1 & \
	pid=$$!; \
	echo "$$pid" >"$(DNS_FORWARDER_PID)"; \
	sleep 1; \
	if ! kill -0 "$$pid" 2>/dev/null; then \
		echo "dns-forwarder failed to start; see $(DNS_FORWARDER_LOG)"; \
		rm -f "$(DNS_FORWARDER_PID)"; \
		exit 1; \
	fi; \
	echo "dns-forwarder started (PID $$pid)"

start-vercel-updater: $(VERCEL_UPDATER_BIN)
	@mkdir -p "$(RUN_DIR)" "$(LOG_DIR)"
	@if [ -f "$(VERCEL_UPDATER_PID)" ]; then \
		pid=$$(cat "$(VERCEL_UPDATER_PID)"); \
		if kill -0 "$$pid" 2>/dev/null && ps -p "$$pid" -o args= | grep -Fq "$(VERCEL_UPDATER_BIN)"; then \
			echo "vercel-updater is already running (PID $$pid)"; \
			exit 0; \
		fi; \
		rm -f "$(VERCEL_UPDATER_PID)"; \
	fi; \
	cd "$(CURDIR)"; \
	nohup "$(VERCEL_UPDATER_BIN)" >>"$(VERCEL_UPDATER_LOG)" 2>&1 & \
	pid=$$!; \
	echo "$$pid" >"$(VERCEL_UPDATER_PID)"; \
	sleep 1; \
	if ! kill -0 "$$pid" 2>/dev/null; then \
		echo "vercel-updater failed to start; see $(VERCEL_UPDATER_LOG)"; \
		rm -f "$(VERCEL_UPDATER_PID)"; \
		exit 1; \
	fi; \
	echo "vercel-updater started (PID $$pid)"

stop: stop-dns-forwarder stop-vercel-updater

stop-dns-forwarder:
	@if [ ! -f "$(DNS_FORWARDER_PID)" ]; then \
		echo "dns-forwarder is not running"; \
	else \
		pid=$$(cat "$(DNS_FORWARDER_PID)"); \
		if kill -0 "$$pid" 2>/dev/null && ps -p "$$pid" -o args= | grep -Fq "$(DNS_FORWARDER_BIN)"; then \
			kill "$$pid"; \
			attempts=0; \
			while kill -0 "$$pid" 2>/dev/null; do \
				if [ "$$attempts" -ge 50 ]; then \
					echo "dns-forwarder did not stop within 5 seconds"; \
					exit 1; \
				fi; \
				attempts=$$((attempts + 1)); \
				sleep 0.1; \
			done; \
			echo "dns-forwarder stopped (PID $$pid)"; \
		else \
			echo "removed stale dns-forwarder PID file"; \
		fi; \
		rm -f "$(DNS_FORWARDER_PID)"; \
	fi

stop-vercel-updater:
	@if [ ! -f "$(VERCEL_UPDATER_PID)" ]; then \
		echo "vercel-updater is not running"; \
	else \
		pid=$$(cat "$(VERCEL_UPDATER_PID)"); \
		if kill -0 "$$pid" 2>/dev/null && ps -p "$$pid" -o args= | grep -Fq "$(VERCEL_UPDATER_BIN)"; then \
			kill "$$pid"; \
			attempts=0; \
			while kill -0 "$$pid" 2>/dev/null; do \
				if [ "$$attempts" -ge 50 ]; then \
					echo "vercel-updater did not stop within 5 seconds"; \
					exit 1; \
				fi; \
				attempts=$$((attempts + 1)); \
				sleep 0.1; \
			done; \
			echo "vercel-updater stopped (PID $$pid)"; \
		else \
			echo "removed stale vercel-updater PID file"; \
		fi; \
		rm -f "$(VERCEL_UPDATER_PID)"; \
	fi

restart:
	@$(MAKE) stop
	@$(MAKE) start

# Run this once after each dns-forwarder rebuild when make start is not run as root.
grant-dns-bind: $(DNS_FORWARDER_BIN)
	sudo setcap cap_net_bind_service=+ep "$(DNS_FORWARDER_BIN)"
