BINARY_NAME=raft-node
BUILD_DIR=bin
LOG_DIR=logs
CMD_PATH=cmd/node/main.go

.PHONY: all build clean run-1 run-2 run-3 start-cluster stop-cluster follow-1 follow-2 follow-3 test-req

all: build

build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)

clean:
	@echo "Cleaning up..."
	rm -rf $(BUILD_DIR)
	rm -rf $(LOG_DIR)

start-cluster: build stop-cluster
	@echo "Starting 3-node cluster..."
	@mkdir -p $(LOG_DIR)
	@$(MAKE) start-1
	@$(MAKE) start-2
	@$(MAKE) start-3
	@echo "Cluster started! Logs in $(LOG_DIR)/"

stop-cluster: stop-1 stop-2 stop-3
	@echo "Cluster stopped."


start-%: build
	@echo "Starting Node $*..."
	@mkdir -p $(LOG_DIR)
	@nohup ./$(BUILD_DIR)/$(BINARY_NAME) -id $* >> $(LOG_DIR)/node$*.log 2>&1 & echo $$! > $(LOG_DIR)/node$*.pid

stop-%:
	@echo "Stopping Node $*..."
	@-kill `cat $(LOG_DIR)/node$*.pid` 2>/dev/null || true
	@rm -f $(LOG_DIR)/node$*.pid
