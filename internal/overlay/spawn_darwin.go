//go:build darwin

package overlay

/*
#include <spawn.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <unistd.h>

// Private macOS API - tells the kernel this spawned process is NOT our TCC
// responsibility. Without this, ScreenCaptureKit and other privacy-gated
// APIs attribute the child's requests back to the parent process, which
// causes permission prompts to target the wrong app (or hang silently if
// the parent doesn't have the permission).
//
// This is the same technique used by Chrome, Slack, Discord, and other
// production Mac apps that spawn helper processes. The symbol lives in
// libSystem so no extra linker flags are needed.
extern int responsibility_spawnattrs_setdisclaim(posix_spawnattr_t *attrs, int disclaim);

// spawnDisclaimed forks + execs `path` with argv/envp using posix_spawn,
// setting the disclaim flag so the child owns its own TCC identity.
// Returns the child's PID on success. On error, returns -1 and the errno
// is set via the return code (negated).
static int spawn_disclaimed(const char *path, char *const argv[], char *const envp[]) {
    posix_spawnattr_t attrs;
    pid_t pid = -1;

    int rc = posix_spawnattr_init(&attrs);
    if (rc != 0) {
        return -rc;
    }

    rc = responsibility_spawnattrs_setdisclaim(&attrs, 1);
    if (rc != 0) {
        posix_spawnattr_destroy(&attrs);
        return -rc;
    }

    rc = posix_spawn(&pid, path, NULL, &attrs, argv, envp);
    posix_spawnattr_destroy(&attrs);
    if (rc != 0) {
        return -rc;
    }
    return (int)pid;
}
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

// spawnDisclaimed launches `path` with `args` (argv[0] defaults to path) as a
// fully TCC-disclaimed child process. The parent does not wait; use the
// returned PID with syscall.Kill for lifecycle management.
//
// This is the only way to spawn a child on macOS such that its
// ScreenCaptureKit / Accessibility / Microphone TCC requests do NOT get
// attributed back to the parent process's identity. Without this, macOS
// "responsible process" tracking walks up the ancestor chain and can cause
// permission prompts to misfire or be silently denied.
func spawnDisclaimed(path string, args []string, extraEnv []string) (int, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	// argv: path first, then the provided args.
	argv := make([]*C.char, 0, len(args)+2)
	argv = append(argv, cPath)
	for _, a := range args {
		cArg := C.CString(a)
		defer C.free(unsafe.Pointer(cArg))
		argv = append(argv, cArg)
	}
	argv = append(argv, nil)

	// envp: inherit from os.Environ + extraEnv.
	env := append(os.Environ(), extraEnv...)
	envp := make([]*C.char, 0, len(env)+1)
	for _, e := range env {
		cEnv := C.CString(e)
		defer C.free(unsafe.Pointer(cEnv))
		envp = append(envp, cEnv)
	}
	envp = append(envp, nil)

	pid := C.spawn_disclaimed(
		cPath,
		(**C.char)(unsafe.Pointer(&argv[0])),
		(**C.char)(unsafe.Pointer(&envp[0])),
	)

	if pid < 0 {
		return 0, fmt.Errorf("posix_spawn (disclaimed): errno=%d", -int(pid))
	}
	return int(pid), nil
}
