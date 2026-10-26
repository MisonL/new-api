use std::{
    collections::VecDeque,
    sync::{
        atomic::{AtomicBool, Ordering},
        Mutex,
    },
};

use tauri::{AppHandle, Manager, Runtime};
use tauri_plugin_shell::process::CommandChild;

use crate::constants::MAX_ERROR_LOG_LINES;

#[derive(Default)]
pub struct DesktopState {
    pub sidecar: Mutex<Option<CommandChild>>,
    pub error_logs: Mutex<VecDeque<String>>,
    pub is_quitting: AtomicBool,
    pub is_stopping_sidecar: AtomicBool,
    pub fatal_error_reported: AtomicBool,
    pub startup_in_progress: AtomicBool,
    pub startup_error: Mutex<Option<String>>,
}

pub fn mark_quitting<R: Runtime>(app: &AppHandle<R>) {
    app.state::<DesktopState>()
        .is_quitting
        .store(true, Ordering::SeqCst);
}

pub fn is_quitting<R: Runtime>(app: &AppHandle<R>) -> bool {
    app.state::<DesktopState>()
        .is_quitting
        .load(Ordering::SeqCst)
}

pub fn mark_stopping_sidecar<R: Runtime>(app: &AppHandle<R>) {
    app.state::<DesktopState>()
        .is_stopping_sidecar
        .store(true, Ordering::SeqCst);
}

pub fn clear_stopping_sidecar<R: Runtime>(app: &AppHandle<R>) {
    app.state::<DesktopState>()
        .is_stopping_sidecar
        .store(false, Ordering::SeqCst);
}

pub fn is_stopping_sidecar<R: Runtime>(app: &AppHandle<R>) -> bool {
    app.state::<DesktopState>()
        .is_stopping_sidecar
        .load(Ordering::SeqCst)
}

/// 只清理由 `terminated_pid` 对应的那个 sidecar。
///
/// 监控线程收到 Terminated 时，主线程可能已经 stop 掉旧进程并通过重试起了
/// 新进程；无条件 take 会把新进程一起杀掉，表现为"重试后立刻又退出"。
pub fn clear_sidecar_if_pid<R: Runtime>(app: &AppHandle<R>, terminated_pid: u32) {
    let state = app.state::<DesktopState>();
    let mut guard = state.sidecar.lock().expect("sidecar state lock poisoned");
    let matches = guard
        .as_ref()
        .is_some_and(|child| should_clear_sidecar(Some(child.pid()), terminated_pid));
    if matches {
        if let Some(child) = guard.take() {
            let _ = child.kill();
        }
    }
}

fn should_clear_sidecar(stored_pid: Option<u32>, terminated_pid: u32) -> bool {
    stored_pid == Some(terminated_pid)
}

pub fn has_running_sidecar<R: Runtime>(app: &AppHandle<R>) -> bool {
    let state = app.state::<DesktopState>();
    let guard = state.sidecar.lock().expect("sidecar state lock poisoned");
    guard.is_some()
}

pub fn try_mark_starting<R: Runtime>(app: &AppHandle<R>) -> bool {
    app.state::<DesktopState>()
        .startup_in_progress
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_ok()
}

pub fn clear_starting<R: Runtime>(app: &AppHandle<R>) {
    app.state::<DesktopState>()
        .startup_in_progress
        .store(false, Ordering::SeqCst);
}

pub fn is_starting<R: Runtime>(app: &AppHandle<R>) -> bool {
    app.state::<DesktopState>()
        .startup_in_progress
        .load(Ordering::SeqCst)
}

pub fn push_error_log<R: Runtime>(app: &AppHandle<R>, line: impl Into<String>) {
    let state = app.state::<DesktopState>();
    let mut guard = state
        .error_logs
        .lock()
        .expect("error log state lock poisoned");
    guard.push_back(line.into());
    while guard.len() > MAX_ERROR_LOG_LINES {
        guard.pop_front();
    }
}

pub fn snapshot_error_logs<R: Runtime>(app: &AppHandle<R>) -> Vec<String> {
    let state = app.state::<DesktopState>();
    let guard = state
        .error_logs
        .lock()
        .expect("error log state lock poisoned");
    guard.iter().cloned().collect()
}

pub fn set_startup_error<R: Runtime>(app: &AppHandle<R>, detail: impl Into<String>) {
    let state = app.state::<DesktopState>();
    let mut guard = state
        .startup_error
        .lock()
        .expect("startup error state lock poisoned");
    *guard = Some(detail.into());
}

pub fn clear_startup_error<R: Runtime>(app: &AppHandle<R>) {
    let state = app.state::<DesktopState>();
    let mut guard = state
        .startup_error
        .lock()
        .expect("startup error state lock poisoned");
    *guard = None;
}

pub fn snapshot_startup_error<R: Runtime>(app: &AppHandle<R>) -> Option<String> {
    let state = app.state::<DesktopState>();
    let guard = state
        .startup_error
        .lock()
        .expect("startup error state lock poisoned");
    guard.clone()
}

#[cfg(test)]
mod tests {
    use super::should_clear_sidecar;

    #[test]
    fn terminated_event_clears_only_the_matching_sidecar() {
        assert!(should_clear_sidecar(Some(4242), 4242));
    }

    #[test]
    fn terminated_event_leaves_a_newer_sidecar_alone() {
        // stop + 重试后，旧进程的 Terminated 事件晚到；新进程必须保留。
        assert!(!should_clear_sidecar(Some(5151), 4242));
    }

    #[test]
    fn terminated_event_with_no_stored_sidecar_is_a_noop() {
        assert!(!should_clear_sidecar(None, 4242));
    }
}
