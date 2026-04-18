use std::{
    fs,
    path::PathBuf,
    sync::{Mutex, MutexGuard, OnceLock},
    time::{SystemTime, UNIX_EPOCH},
};

use new_api_tauri_desktop_lib::runtime_support::{
    analyze_startup_error, is_ready_status_response, is_service_management_recoverable_error,
    load_or_create_desktop_runtime_config, load_or_create_desktop_secrets,
    probe_readiness_response, resolve_single_instance_focus_target, save_desktop_runtime_config,
    DesktopRuntimeConfig, ReadinessProbeResult, SingleInstanceFocusTarget,
};

fn desktop_port_env_lock() -> MutexGuard<'static, ()> {
    static LOCK: OnceLock<Mutex<()>> = OnceLock::new();
    LOCK.get_or_init(|| Mutex::new(()))
        .lock()
        .expect("desktop port env lock poisoned")
}

struct DesktopPortEnvGuard {
    _lock: MutexGuard<'static, ()>,
    previous_value: Option<String>,
}

impl DesktopPortEnvGuard {
    fn clear() -> Self {
        let lock = desktop_port_env_lock();
        let previous_value = std::env::var("NEW_API_DESKTOP_PORT").ok();
        std::env::remove_var("NEW_API_DESKTOP_PORT");
        Self {
            _lock: lock,
            previous_value,
        }
    }
}

impl Drop for DesktopPortEnvGuard {
    fn drop(&mut self) {
        match &self.previous_value {
            Some(value) => std::env::set_var("NEW_API_DESKTOP_PORT", value),
            None => std::env::remove_var("NEW_API_DESKTOP_PORT"),
        }
    }
}

#[test]
fn load_or_create_desktop_secrets_persists_stable_values() {
    let test_dir = unique_test_dir("desktop-secrets-stable");
    let secret_file = test_dir.join("desktop-secrets.json");

    let first = load_or_create_desktop_secrets(&secret_file).expect("first secret load must work");
    let second =
        load_or_create_desktop_secrets(&secret_file).expect("second secret load must work");

    assert_eq!(first, second);
    assert!(secret_file.exists());
    assert_eq!(first.session_secret.len(), 64);
    assert_eq!(first.crypto_secret.len(), 64);

    cleanup_test_dir(&test_dir);
}

#[test]
fn load_or_create_desktop_secrets_rejects_invalid_file() {
    let test_dir = unique_test_dir("desktop-secrets-invalid");
    let secret_file = test_dir.join("desktop-secrets.json");
    fs::create_dir_all(&test_dir).expect("test directory must be created");
    fs::write(
        &secret_file,
        r#"{"version":1,"session_secret":"","crypto_secret":"random_string"}"#,
    )
    .expect("invalid secret file must be written");

    let error =
        load_or_create_desktop_secrets(&secret_file).expect_err("invalid secret file must fail");
    assert!(error.contains("desktop secret file is invalid"));

    cleanup_test_dir(&test_dir);
}

#[test]
fn load_or_create_desktop_runtime_config_persists_default_port() {
    let _guard = DesktopPortEnvGuard::clear();
    let test_dir = unique_test_dir("desktop-runtime-default");
    let config_file = test_dir.join("desktop-runtime.json");

    let config = load_or_create_desktop_runtime_config(&config_file)
        .expect("desktop runtime config must be created");

    assert_eq!(config.port, 3000);
    assert!(config_file.exists());

    let second = load_or_create_desktop_runtime_config(&config_file)
        .expect("desktop runtime config must be loaded");
    assert_eq!(second.port, 3000);

    cleanup_test_dir(&test_dir);
}

#[test]
fn load_or_create_desktop_runtime_config_reads_existing_custom_port() {
    let _guard = DesktopPortEnvGuard::clear();
    let test_dir = unique_test_dir("desktop-runtime-custom");
    let config_file = test_dir.join("desktop-runtime.json");
    fs::create_dir_all(&test_dir).expect("test directory must be created");
    fs::write(&config_file, r#"{"version":1,"port":13000}"#)
        .expect("desktop runtime config must be written");

    let config = load_or_create_desktop_runtime_config(&config_file)
        .expect("desktop runtime config must be loaded");

    assert_eq!(config.port, 13000);

    cleanup_test_dir(&test_dir);
}

#[test]
fn load_or_create_desktop_runtime_config_rejects_invalid_file() {
    let _guard = DesktopPortEnvGuard::clear();
    let test_dir = unique_test_dir("desktop-runtime-invalid");
    let config_file = test_dir.join("desktop-runtime.json");
    fs::create_dir_all(&test_dir).expect("test directory must be created");
    fs::write(&config_file, r#"{"version":1,"port":0}"#)
        .expect("desktop runtime config must be written");

    let error = load_or_create_desktop_runtime_config(&config_file)
        .expect_err("invalid desktop runtime config must fail");

    assert!(error.contains("desktop runtime config is invalid"));

    cleanup_test_dir(&test_dir);
}

#[test]
fn save_desktop_runtime_config_persists_updated_port() {
    let _guard = DesktopPortEnvGuard::clear();
    let test_dir = unique_test_dir("desktop-runtime-save");
    let config_file = test_dir.join("desktop-runtime.json");
    fs::create_dir_all(&test_dir).expect("test directory must be created");

    save_desktop_runtime_config(&config_file, &DesktopRuntimeConfig { port: 14567 })
        .expect("desktop runtime config must be saved");

    let config = load_or_create_desktop_runtime_config(&config_file)
        .expect("desktop runtime config must be loaded");
    assert_eq!(config.port, 14567);

    cleanup_test_dir(&test_dir);
}

#[test]
fn load_or_create_desktop_runtime_config_uses_env_port_when_config_missing() {
    let _guard = DesktopPortEnvGuard::clear();
    let test_dir = unique_test_dir("desktop-runtime-env-fallback");
    let config_file = test_dir.join("desktop-runtime.json");
    std::env::set_var("NEW_API_DESKTOP_PORT", "13000");

    let config = load_or_create_desktop_runtime_config(&config_file)
        .expect("desktop runtime config must load from env");

    assert_eq!(config.port, 13000);
    assert!(config_file.exists());
    cleanup_test_dir(&test_dir);
}

#[test]
fn load_or_create_desktop_runtime_config_rejects_invalid_env_port() {
    let _guard = DesktopPortEnvGuard::clear();
    let test_dir = unique_test_dir("desktop-runtime-invalid-env");
    let config_file = test_dir.join("desktop-runtime.json");
    std::env::set_var("NEW_API_DESKTOP_PORT", "0");

    let error = load_or_create_desktop_runtime_config(&config_file)
        .expect_err("invalid env port must fail");

    assert!(error.contains("invalid NEW_API_DESKTOP_PORT"));
    cleanup_test_dir(&test_dir);
}

#[test]
fn load_or_create_desktop_runtime_config_prefers_existing_file_over_env() {
    let _guard = DesktopPortEnvGuard::clear();
    let test_dir = unique_test_dir("desktop-runtime-config-priority");
    let config_file = test_dir.join("desktop-runtime.json");
    fs::create_dir_all(&test_dir).expect("test directory must be created");
    fs::write(&config_file, r#"{"version":1,"port":13000}"#)
        .expect("desktop runtime config must be written");
    std::env::set_var("NEW_API_DESKTOP_PORT", "14000");

    let config = load_or_create_desktop_runtime_config(&config_file)
        .expect("desktop runtime config must prefer persisted config");

    assert_eq!(config.port, 13000);
    cleanup_test_dir(&test_dir);
}

#[test]
fn is_ready_status_response_accepts_success_payload() {
    let response =
        "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"success\":true,\"version\":\"1.0.0\"}";
    assert!(is_ready_status_response(response));
}

#[test]
fn is_ready_status_response_rejects_non_ready_payload() {
    let non_200 =
        "HTTP/1.1 503 Service Unavailable\r\nContent-Type: application/json\r\n\r\n{\"success\":true,\"version\":\"1.0.0\"}";
    let missing_version =
        "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"success\":true}";
    let missing_success =
        "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"version\":\"1.0.0\"}";

    assert!(!is_ready_status_response(non_200));
    assert!(!is_ready_status_response(missing_version));
    assert!(!is_ready_status_response(missing_success));
}

#[test]
fn probe_readiness_response_reports_unexpected_service() {
    let response =
        "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n<html><body>nginx landing page</body></html>";
    let result = probe_readiness_response(response);

    assert_eq!(
        result,
        ReadinessProbeResult::NotReady {
            observation:
                "status=HTTP/1.1 200 OK; body_prefix=<html><body>nginx landing page</body></html>"
                    .to_string(),
        }
    );
}

#[test]
fn analyze_startup_error_identifies_unexpected_http_service_on_port_3000() {
    let detail = "timed out waiting for ready local server at 127.0.0.1:3000/api/status; last probe observation: status=HTTP/1.1 200 OK; body_prefix=<html>occupied</html>";
    let diagnosis = analyze_startup_error(detail, &[]);

    assert_eq!(diagnosis.title, "Unexpected service responded on port 3000");
    assert!(diagnosis.summary.contains("did not look like New API"));
    assert!(diagnosis.detail.contains("Last probe observation"));
}

#[test]
fn analyze_startup_error_identifies_invalid_runtime_config() {
    let diagnosis = analyze_startup_error(
        "failed to bootstrap desktop runtime: failed to parse desktop runtime config: expected value at line 1 column 1",
        &[],
    );

    assert_eq!(diagnosis.title, "Desktop runtime config is invalid");
    assert!(diagnosis.summary.contains("runtime configuration"));
}

#[test]
fn analyze_startup_error_identifies_preflight_port_conflict() {
    let detail = "port 127.0.0.1:3000 is already in use before starting desktop sidecar";
    let diagnosis = analyze_startup_error(detail, &[]);

    assert_eq!(diagnosis.title, "Port 3000 is already in use");
    assert!(diagnosis.summary.contains("port 3000 is already occupied"));
}

#[test]
fn analyze_startup_error_identifies_preflight_custom_port_conflict() {
    let detail = "port 127.0.0.1:13000 is already in use before starting desktop sidecar";
    let diagnosis = analyze_startup_error(detail, &[]);

    assert_eq!(diagnosis.title, "Port 13000 is already in use");
    assert!(diagnosis.detail.contains("13000"));
}

#[test]
fn analyze_startup_error_keeps_generic_timeout_for_probe_errors() {
    let detail = "timed out waiting for ready local server at 127.0.0.1:3000/api/status; last probe observation: probe error: connection refused";
    let diagnosis = analyze_startup_error(detail, &[]);

    assert_eq!(diagnosis.title, "Local server did not become ready");
}

#[test]
fn service_management_recoverable_error_only_matches_port_recovery_cases() {
    let occupied_port = analyze_startup_error(
        "port 127.0.0.1:3000 is already in use before starting desktop sidecar",
        &[],
    );
    let unexpected_service = analyze_startup_error(
        "timed out waiting for ready local server at 127.0.0.1:13000/api/status; last probe observation: status=HTTP/1.1 200 OK; body_prefix=<html>occupied</html>",
        &[],
    );
    let generic_failure = analyze_startup_error(
        "timed out waiting for ready local server at 127.0.0.1:3000/api/status; last probe observation: probe error: connection refused",
        &[],
    );
    let invalid_runtime_config = analyze_startup_error(
        "failed to bootstrap desktop runtime: desktop runtime config is invalid: /tmp/desktop-runtime.json",
        &[],
    );

    assert!(is_service_management_recoverable_error(&occupied_port));
    assert!(is_service_management_recoverable_error(&unexpected_service));
    assert!(is_service_management_recoverable_error(
        &invalid_runtime_config
    ));
    assert!(!is_service_management_recoverable_error(&generic_failure));
}

#[test]
fn resolve_single_instance_focus_target_prefers_main_window() {
    assert_eq!(
        resolve_single_instance_focus_target(true, true),
        SingleInstanceFocusTarget::MainWindow,
    );
    assert_eq!(
        resolve_single_instance_focus_target(true, false),
        SingleInstanceFocusTarget::MainWindow,
    );
}

#[test]
fn resolve_single_instance_focus_target_uses_service_management_when_main_missing() {
    assert_eq!(
        resolve_single_instance_focus_target(false, true),
        SingleInstanceFocusTarget::ServiceManagementWindow,
    );
}

#[test]
fn resolve_single_instance_focus_target_noops_without_existing_windows() {
    assert_eq!(
        resolve_single_instance_focus_target(false, false),
        SingleInstanceFocusTarget::None,
    );
}

fn unique_test_dir(prefix: &str) -> PathBuf {
    let timestamp = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system time must be valid")
        .as_nanos();
    std::env::temp_dir().join(format!(
        "new-api-tauri-{prefix}-{}-{timestamp}",
        std::process::id()
    ))
}

fn cleanup_test_dir(path: &PathBuf) {
    if path.exists() {
        fs::remove_dir_all(path).expect("test directory must be removable");
    }
}
