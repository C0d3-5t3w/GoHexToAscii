.PHONY: all build clean run

# Project variables
BINARY_NAME=GoHexToAscii
SRC_DIR=./cmd

all: build

build:
	@go build -o $(BINARY_NAME) $(SRC_DIR)/main.go

clean:
	@rm -f $(BINARY_NAME)

run: build
	@./$(BINARY_NAME)
