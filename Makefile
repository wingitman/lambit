BINARY      := lambit
INSTALL_DIR := $(HOME)/.local/bin
BUILD_DIR   := bin
COMMIT      := $(shell git rev-parse HEAD 2>/dev/null || printf dev)

.PHONY: all build install install-shell uninstall clean test test-integration test-all cross-build

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="-s -w -X github.com/wingitman/lambit/internal/version.Commit=$(COMMIT)" -o $(BUILD_DIR)/$(BINARY) .
	@echo "Built: $(BUILD_DIR)/$(BINARY)"

install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed: $(INSTALL_DIR)/$(BINARY)"
	@echo ""
	@$(MAKE) --no-print-directory install-shell

install-shell:
	@# --- zsh ---
	@if [ -f "$(HOME)/.zshrc" ]; then \
		if ! grep -q '\.local/bin' "$(HOME)/.zshrc"; then \
			echo "" >> "$(HOME)/.zshrc"; \
			echo 'export PATH="$$HOME/.local/bin:$$PATH"' >> "$(HOME)/.zshrc"; \
			echo "Added ~/.local/bin to PATH in ~/.zshrc"; \
		else \
			echo "~/.zshrc already has ~/.local/bin in PATH"; \
		fi \
	fi
	@# --- bash ---
	@if [ -f "$(HOME)/.bashrc" ]; then \
		if ! grep -q '\.local/bin' "$(HOME)/.bashrc"; then \
			echo "" >> "$(HOME)/.bashrc"; \
			echo 'export PATH="$$HOME/.local/bin:$$PATH"' >> "$(HOME)/.bashrc"; \
			echo "Added ~/.local/bin to PATH in ~/.bashrc"; \
		else \
			echo "~/.bashrc already has ~/.local/bin in PATH"; \
		fi \
	fi
	@# --- fish ---
	@if [ -f "$(HOME)/.config/fish/config.fish" ]; then \
		if ! grep -q '\.local/bin' "$(HOME)/.config/fish/config.fish"; then \
			echo "" >> "$(HOME)/.config/fish/config.fish"; \
			echo "fish_add_path \$$HOME/.local/bin" >> "$(HOME)/.config/fish/config.fish"; \
			echo "Added ~/.local/bin to PATH in config.fish"; \
		else \
			echo "config.fish already has ~/.local/bin in PATH"; \
		fi \
	fi
	@# --- powershell (cross-platform pwsh on macOS/Linux) ---
	@if [ -f "$(HOME)/.config/powershell/Microsoft.PowerShell_profile.ps1" ]; then \
		if ! grep -q '\.local/bin' "$(HOME)/.config/powershell/Microsoft.PowerShell_profile.ps1"; then \
			echo "" >> "$(HOME)/.config/powershell/Microsoft.PowerShell_profile.ps1"; \
			echo '$$env:PATH = "$$HOME/.local/bin:$$env:PATH"' >> "$(HOME)/.config/powershell/Microsoft.PowerShell_profile.ps1"; \
			echo "Added ~/.local/bin to PATH in PowerShell profile"; \
		else \
			echo "PowerShell profile already has ~/.local/bin in PATH"; \
		fi \
	fi
	@echo ""
	@echo "Reload your shell or run: source ~/.zshrc  (or ~/.bashrc / exec fish / . \$$PROFILE for pwsh)"
	@echo "Then type 'lambit' to launch."

uninstall:
	@rm -f $(INSTALL_DIR)/$(BINARY)
	@echo "Removed $(INSTALL_DIR)/$(BINARY)"
	@echo "Note: any PATH lines added to your shell rc files remain — remove them manually if desired."

clean:
	rm -rf $(BUILD_DIR)

# Unit tests only (fast, no PTY required, safe for CI)
test:
	go test ./... -timeout 30s

# Integration tests (require a real PTY / terminal)
test-integration:
	go test -tags integration -timeout 60s -v .

# Run everything
test-all: test test-integration

# Cross-compile release binaries for all supported platforms
cross-build:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin  GOARCH=arm64  go build -ldflags="-s -w -X github.com/wingitman/lambit/internal/version.Commit=$(COMMIT)" -o $(BUILD_DIR)/$(BINARY)-macos-arm64 .
	GOOS=darwin  GOARCH=amd64  go build -ldflags="-s -w -X github.com/wingitman/lambit/internal/version.Commit=$(COMMIT)" -o $(BUILD_DIR)/$(BINARY)-macos-amd64 .
	GOOS=linux   GOARCH=amd64  go build -ldflags="-s -w -X github.com/wingitman/lambit/internal/version.Commit=$(COMMIT)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64 .
	GOOS=linux   GOARCH=arm64  go build -ldflags="-s -w -X github.com/wingitman/lambit/internal/version.Commit=$(COMMIT)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64 .
	GOOS=windows GOARCH=amd64  go build -ldflags="-s -w -X github.com/wingitman/lambit/internal/version.Commit=$(COMMIT)" -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe .
	@echo "Cross-compiled binaries written to $(BUILD_DIR)/"
