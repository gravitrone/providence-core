package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckBashSecurity_SafeCommands(t *testing.T) {
	safeCommands := []string{
		"echo hello",
		"ls -la",
		"git status",
		"cat /etc/hosts",
		"go build ./...",
		"npm install",
		"cd /tmp && ls",
		"grep -r 'pattern' .",
		"find . -name '*.go'",
		"rm -rf /tmp/test-dir",
		"docker ps",
		"curl https://example.com",
	}

	for _, cmd := range safeCommands {
		t.Run(cmd, func(t *testing.T) {
			check := CheckBashSecurity(cmd)
			assert.True(t, check.Allowed, "expected safe but blocked: %s (reason: %s)", cmd, check.Reason)
		})
	}
}

func TestCheckBashSecurity_EmptyCommand(t *testing.T) {
	check := CheckBashSecurity("")
	assert.True(t, check.Allowed)

	check = CheckBashSecurity("   ")
	assert.True(t, check.Allowed)
}

func TestCheckBashSecurity_BlocksZshDangerous(t *testing.T) {
	dangerous := []struct {
		cmd    string
		substr string
	}{
		{"zmodload zsh/system", "zmodload"},
		{"emulate -c 'evil code'", "emulate"},
		{"sysopen /etc/passwd", "sysopen"},
		{"sysread 3", "sysread"},
		{"syswrite 3 data", "syswrite"},
		{"ztcp localhost 8080", "ztcp"},
		{"zsocket /tmp/sock", "zsocket"},
		{"zpty evil bash", "zpty"},
		{"zf_rm /important", "zf_rm"},
		{"zf_chmod 777 /etc/shadow", "zf_chmod"},
	}

	for _, tc := range dangerous {
		t.Run(tc.cmd, func(t *testing.T) {
			check := CheckBashSecurity(tc.cmd)
			assert.False(t, check.Allowed, "expected blocked: %s", tc.cmd)
			assert.Contains(t, check.Reason, tc.substr)
		})
	}
}

func TestCheckBashSecurity_BlocksEval(t *testing.T) {
	check := CheckBashSecurity("eval $(cat /tmp/script)")
	assert.False(t, check.Allowed)
	assert.Contains(t, check.Reason, "eval")
}

func TestCheckBashSecurity_BlocksDevAccess(t *testing.T) {
	devCmds := []string{
		"cat /dev/sda",
		"dd if=/dev/zero of=/dev/sda",
		"echo test > /dev/hda1",
		"cat /dev/mem",
		"hexdump /dev/kmem",
	}

	for _, cmd := range devCmds {
		t.Run(cmd, func(t *testing.T) {
			check := CheckBashSecurity(cmd)
			assert.False(t, check.Allowed, "expected blocked: %s", cmd)
			assert.Contains(t, check.Reason, "/dev/")
		})
	}
}

func TestCheckBashSecurity_AllowsDevNull(t *testing.T) {
	// /dev/null is NOT in the blocked device pattern
	check := CheckBashSecurity("echo test > /dev/null")
	assert.True(t, check.Allowed, "should allow /dev/null redirect")
}

func TestCheckBashSecurity_BlocksProcEnviron(t *testing.T) {
	cmds := []string{
		"cat /proc/1/environ",
		"cat /proc/self/environ",
		"strings /proc/1234/environ",
	}

	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			check := CheckBashSecurity(cmd)
			assert.False(t, check.Allowed, "expected blocked: %s", cmd)
			assert.Contains(t, check.Reason, "environ")
		})
	}
}

func TestCheckBashSecurity_BlocksDestructive(t *testing.T) {
	cmds := []string{
		"rm -rf / ",
		"rm -f / ",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda bs=512 count=1",
	}

	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			check := CheckBashSecurity(cmd)
			assert.False(t, check.Allowed, "expected blocked: %s", cmd)
		})
	}
}

func TestCheckBashSecurity_BlocksSedBacktick(t *testing.T) {
	check := CheckBashSecurity("sed 's/old/`rm -rf /`/' file.txt")
	assert.False(t, check.Allowed)
	assert.Contains(t, check.Reason, "sed")
}

func TestCheckBashSecurity_BlocksControlChars(t *testing.T) {
	check := CheckBashSecurity("echo \x07hello")
	assert.False(t, check.Allowed)
	assert.Contains(t, check.Reason, "control characters")
}

func TestCheckBashSecurity_PipelineSplit(t *testing.T) {
	// zmodload after a pipe should still be caught
	check := CheckBashSecurity("echo test | zmodload zsh/system")
	assert.False(t, check.Allowed)
	assert.Contains(t, check.Reason, "zmodload")

	// zmodload after && should still be caught
	check = CheckBashSecurity("echo test && sysopen /etc/passwd")
	assert.False(t, check.Allowed)
	assert.Contains(t, check.Reason, "sysopen")

	// eval after semicolon
	check = CheckBashSecurity("echo ok; eval dangerous")
	assert.False(t, check.Allowed)
	assert.Contains(t, check.Reason, "eval")
}

func TestCheckBashSecurity_EnvVarPrefix(t *testing.T) {
	// VAR=val before a dangerous command should still catch it
	check := CheckBashSecurity("FOO=bar zmodload zsh/system")
	assert.False(t, check.Allowed)
	assert.Contains(t, check.Reason, "zmodload")
}

func TestCheckBashSecurity_WiredIntoBash(t *testing.T) {
	b := NewBashTool()
	res := b.Execute(context.Background(), map[string]any{
		"command": "zmodload zsh/system",
	})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "security check")
}

func TestSplitCommandSegments(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"echo hello", []string{"echo hello"}},
		{"echo a && echo b", []string{"echo a", "echo b"}},
		{"echo a; echo b", []string{"echo a", "echo b"}},
		{"echo a | grep b", []string{"echo a", "grep b"}},
		{"echo a || echo b", []string{"echo a", "echo b"}},
		{"echo 'a;b'", []string{"echo 'a;b'"}},
		{`echo "a && b"`, []string{`echo "a && b"`}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			segments := splitCommandSegments(tc.input)
			assert.Equal(t, tc.expected, segments)
		})
	}
}

func TestExtractBaseCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"echo hello", "echo"},
		{"  ls -la ", "ls"},
		{"FOO=bar git status", "git"},
		{"VAR1=a VAR2=b cmd arg", "cmd"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			base := extractBaseCommand(tc.input)
			assert.Equal(t, tc.expected, base)
		})
	}
}
