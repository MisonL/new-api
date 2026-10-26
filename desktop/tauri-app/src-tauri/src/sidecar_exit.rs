//! Graceful sidecar termination.
//!
//! tauri-plugin-shell's `CommandChild::kill` ultimately calls
//! `shared_child::SharedChild::kill`, which on Unix sends SIGKILL. The Go
//! sidecar only flushes its in-memory batch deltas and dashboard aggregates
//! from its SIGINT/SIGTERM handler (main.go); a SIGKILL skips that entirely,
//! so every normal quit loses up to one batch interval of quota increments.
//!
//! On Unix we therefore send SIGTERM and wait a bounded time for the process
//! to exit on its own, falling back to the plugin's kill only when the
//! graceful window expires. On Windows there is no portable SIGTERM; the job
//! object / TerminateProcess path is the only option.

#[cfg(unix)]
mod platform {
    use std::time::{Duration, Instant};

    use tauri_plugin_shell::process::CommandChild;

    /// How long the sidecar gets to finish its shutdown handler before the
    /// process is killed outright. main.go's own SHUTDOWN_TIMEOUT_SECONDS
    /// defaults to 120s, but that bounds draining in-flight requests; a
    /// desktop quit path waits only for the batch flush that follows it.
    const GRACEFUL_EXIT_TIMEOUT: Duration = Duration::from_secs(10);
    const POLL_INTERVAL: Duration = Duration::from_millis(50);

    /// Sends SIGTERM, then polls until the child exits or the grace period
    /// expires. Returns true when the process exited without the plugin's
    /// kill after the signal.
    pub fn terminate_gracefully(child: &CommandChild) -> bool {
        let pid = child.pid();
        if pid == 0 {
            return false;
        }
        // SAFETY: kill(2) with SIGTERM on a pid we spawned; a stale pid is
        // rejected by the kernel with ESRCH, which we treat as already gone.
        let sent = unsafe { libc::kill(pid as libc::pid_t, libc::SIGTERM) };
        if sent != 0 {
            let err = std::io::Error::last_os_error();
            if err.raw_os_error() == Some(libc::ESRCH) {
                return true;
            }
            return false;
        }
        let deadline = Instant::now() + GRACEFUL_EXIT_TIMEOUT;
        while Instant::now() < deadline {
            if let Some(true) = process_exited(pid) {
                return true;
            }
            std::thread::sleep(POLL_INTERVAL);
        }
        false
    }

    /// Returns Some(true) when the pid is gone, Some(false) when it is still
    /// alive, None when the check itself failed (treated as "keep waiting").
    fn process_exited(pid: u32) -> Option<bool> {
        // SAFETY: signal 0 performs existence/permission checks only.
        let alive = unsafe { libc::kill(pid as libc::pid_t, 0) };
        if alive == 0 {
            return Some(false);
        }
        let err = std::io::Error::last_os_error();
        match err.raw_os_error() {
            Some(libc::ESRCH) => Some(true),
            Some(libc::EPERM) => Some(false),
            _ => None,
        }
    }
}

#[cfg(not(unix))]
mod platform {
    use tauri_plugin_shell::process::CommandChild;

    /// Windows has no portable graceful signal for a child process; the
    /// caller falls back to the plugin's termination path.
    pub fn terminate_gracefully(_child: &CommandChild) -> bool {
        false
    }
}

/// Stops the sidecar, preferring a graceful SIGTERM on Unix and falling back
/// to the plugin's kill when the grace period expires or the platform has no
/// signal-based shutdown.
///
/// The plugin's kill is always called at the end: it also waits for the
/// child, which frees the port before the next launch preflights it, and it
/// is a no-op once the process has already exited.
pub fn stop(child: tauri_plugin_shell::process::CommandChild) {
    platform::terminate_gracefully(&child);
    let _ = child.kill();
}
