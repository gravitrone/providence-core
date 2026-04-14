BINARY := .build/release/providence-mac-bridge
INSTALL_DIR := $(HOME)/.providence/bin

.PHONY: build install clean test

build:
	swift build -c release
	codesign --force --deep --sign - $(BINARY)

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/

clean:
	swift package clean

test:
	swift test
