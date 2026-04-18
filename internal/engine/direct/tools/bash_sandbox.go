package tools

import (
	"fmt"
	"os"
	"strings"

	"github.com/gravitrone/providence-core/internal/config"
)

const sandboxResolverSocket = "/private/var/run/mDNSResponder"

func (b *BashTool) ensureSandboxProfile() (string, error) {
	if path := b.sandboxProfileFile(); path != "" {
		return path, nil
	}

	root, err := b.resolveSessionRoot()
	if err != nil {
		return "", fmt.Errorf("resolve bash session root: %w", err)
	}

	profile, err := b.renderSandboxProfile(root)
	if err != nil {
		return "", err
	}

	sessionKey := b.sessionKey()
	file, err := os.CreateTemp("", fmt.Sprintf("providence-bash-%s-*.sb", sanitiseForPath(sessionKey)))
	if err != nil {
		return "", fmt.Errorf("create bash sandbox profile: %w", err)
	}

	path := file.Name()
	writeErr := error(nil)
	if _, err := file.WriteString(profile); err != nil {
		writeErr = fmt.Errorf("write bash sandbox profile: %w", err)
	}
	if err := file.Close(); err != nil && writeErr == nil {
		writeErr = fmt.Errorf("close bash sandbox profile: %w", err)
	}
	if writeErr != nil {
		_ = os.Remove(path)
		return "", writeErr
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sandboxFile != "" {
		_ = os.Remove(path)
		return b.sandboxFile, nil
	}
	b.sandboxFile = path
	return b.sandboxFile, nil
}

func (b *BashTool) sandboxProfileFile() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.sandboxFile
}

func (b *BashTool) renderSandboxProfile(root string) (string, error) {
	cfg := config.LoadMerged(root)
	normalized, err := config.NormalizeSandboxConfig(cfg.Sandbox)
	if err != nil {
		return "", fmt.Errorf("validate sandbox config: %w", err)
	}

	var lines []string
	lines = append(lines,
		"(version 1)",
		"(allow default)",
		"(deny network*)",
		`(deny file-write* (subpath "/System"))`,
	)

	if len(normalized.AllowNetwork) > 0 {
		lines = append(lines, "(allow network-outbound")
		lines = append(lines, fmt.Sprintf(`       (literal %q)`, sandboxResolverSocket))
		for _, entry := range normalized.AllowNetwork {
			lines = append(lines, fmt.Sprintf(`       (remote tcp %q)`, entry))
			lines = append(lines, fmt.Sprintf(`       (remote udp %q)`, entry))
		}
		lines = append(lines, ")")
	}

	for _, path := range normalized.AllowWrite {
		lines = append(lines, fmt.Sprintf(`(allow file-write* (subpath %q))`, path))
	}

	return strings.Join(lines, "\n") + "\n", nil
}
