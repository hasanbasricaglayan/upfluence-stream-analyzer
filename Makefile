APP_NAME=upfluence-stream-analyzer
BIN_DIR=bin
CMD_DIR=cmd

.PHONY: run
run:
	go run ./${CMD_DIR}

.PHONY: build
build:
	go build -o ${BIN_DIR}/${APP_NAME} ./${CMD_DIR}
