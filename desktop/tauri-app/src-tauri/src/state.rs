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

pub fn clear_sidecar<R: Runtime>(app: &AppHandle<R>) {
    let state = app.state::<DesktopState>();
    let mut guard = state.sidecar.lock().expect("sidecar state lock poisoned");
    if let Some(child) = guard.take() {
        let _ = child.kill();
    }
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
