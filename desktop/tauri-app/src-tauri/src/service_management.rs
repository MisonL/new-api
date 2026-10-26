use std::{borrow::Cow, path::PathBuf, process::Command};

use serde::{Deserialize, Serialize};
#[cfg(target_os = "windows")]
use serde_json::Value;
use tauri::{
    http::{header, Method, Response, StatusCode},
    AppHandle, Manager, Runtime, UriSchemeContext,
};

use crate::{
    constants::DESKTOP_RUNTIME_CONFIG_FILE_NAME,
    runtime,
    runtime_support::{
        analyze_startup_error, load_or_create_desktop_runtime_config, save_desktop_runtime_config,
        DesktopRuntimeConfig, ErrorDiagnosis,
    },
    state::{has_running_sidecar, is_starting, snapshot_error_logs, snapshot_startup_error},
    windowing,
};

const SERVICE_MANAGEMENT_HTML: &str = include_str!("../assets/service_management.html");

/// 页面只有内联样式和内联脚本，只 fetch 同源的 /api/*，所以可以收紧到
/// 默认拒绝：不需要远程脚本、远程样式、字体、图片或框架。
const SERVICE_MANAGEMENT_CSP: &str = "default-src 'none'; script-src 'unsafe-inline'; \
     style-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; \
     base-uri 'none'; form-action 'none'; frame-ancestors 'none'";

/// 状态变更请求必须带的自定义头。
const SERVICE_MANAGEMENT_REQUEST_HEADER: &str = "x-newapi-service-request";

#[derive(Debug, Serialize)]
struct ServiceManagementState {
    startup_status: String,
    app_data_dir: String,
    config_path: String,
    port: Option<u16>,
    config_error: Option<String>,
    startup_diagnosis: Option<ErrorDiagnosis>,
    port_processes: Vec<PortProcessInfo>,
}

#[derive(Debug, Serialize)]
struct PortProcessInfo {
    pid: u32,
    name: String,
    endpoint: String,
    command: String,
}

#[derive(Debug, Deserialize)]
struct UpdatePortRequest {
    port: u16,
}

#[derive(Debug, Serialize)]
struct ActionResponse {
    message: String,
}

pub fn handle_request<R: Runtime>(
    ctx: UriSchemeContext<'_, R>,
    request: tauri::http::Request<Vec<u8>>,
) -> Response<Cow<'static, [u8]>> {
    let path = request.uri().path();

    if !is_state_changing_request_authorized(request.method(), request.headers()) {
        return text_response(
            StatusCode::FORBIDDEN,
            "missing service management request header",
        );
    }

    match (request.method(), path) {
        (&Method::GET, "/") | (&Method::GET, "/index.html") => {
            html_response(SERVICE_MANAGEMENT_HTML)
        }
        (&Method::GET, "/api/state") => {
            json_response(build_service_management_state(ctx.app_handle()))
        }
        (&Method::POST, "/api/config") => {
            match serde_json::from_slice::<UpdatePortRequest>(request.body()) {
                Ok(payload) => match update_runtime_config(ctx.app_handle(), payload.port) {
                    Ok(message) => json_response(ActionResponse { message }),
                    Err(err) => text_response(StatusCode::BAD_REQUEST, err),
                },
                Err(err) => text_response(
                    StatusCode::BAD_REQUEST,
                    format!("invalid config request body: {err}"),
                ),
            }
        }
        (&Method::POST, "/api/retry") => match runtime::request_startup_retry(ctx.app_handle()) {
            Ok(message) => json_response(ActionResponse { message }),
            Err(err) => text_response(StatusCode::BAD_REQUEST, err),
        },
        (&Method::POST, "/api/open-data-dir") => {
            match windowing::open_app_data_dir(ctx.app_handle()) {
                Ok(()) => json_response(ActionResponse {
                    message: "opened app data directory".to_string(),
                }),
                Err(err) => text_response(StatusCode::INTERNAL_SERVER_ERROR, err.to_string()),
            }
        }
        (&Method::POST, "/api/open-log-dir") => match windowing::open_log_dir(ctx.app_handle()) {
            Ok(()) => json_response(ActionResponse {
                message: "opened log directory".to_string(),
            }),
            Err(err) => text_response(StatusCode::INTERNAL_SERVER_ERROR, err.to_string()),
        },
        _ => text_response(StatusCode::NOT_FOUND, "not found"),
    }
}

fn build_service_management_state<R: Runtime>(app: &AppHandle<R>) -> ServiceManagementState {
    let app_data_dir = app
        .path()
        .app_data_dir()
        .map(|path| path.to_string_lossy().to_string())
        .unwrap_or_default();
    let config_path = app
        .path()
        .app_data_dir()
        .map(|path| path.join(DESKTOP_RUNTIME_CONFIG_FILE_NAME))
        .unwrap_or_else(|_| PathBuf::from(DESKTOP_RUNTIME_CONFIG_FILE_NAME));
    let (runtime_config, config_error) = match load_or_create_desktop_runtime_config(&config_path) {
        Ok(config) => (Some(config), None),
        Err(err) => (None, Some(err)),
    };
    let startup_error = snapshot_startup_error(app);
    let startup_diagnosis = startup_error
        .as_ref()
        .map(|detail| analyze_startup_error(detail, &snapshot_error_logs(app)));
    let startup_status = if has_running_sidecar(app) {
        "running".to_string()
    } else if is_starting(app) {
        "starting".to_string()
    } else if startup_error.is_some() {
        "error".to_string()
    } else {
        "stopped".to_string()
    };

    ServiceManagementState {
        startup_status,
        app_data_dir,
        config_path: config_path.to_string_lossy().to_string(),
        port: runtime_config.as_ref().map(|config| config.port),
        config_error,
        startup_diagnosis,
        port_processes: runtime_config
            .map(|config| list_listening_processes_on_port(config.port))
            .unwrap_or_default(),
    }
}

fn update_runtime_config<R: Runtime>(app: &AppHandle<R>, port: u16) -> Result<String, String> {
    if port == 0 {
        return Err("invalid desktop port: 0".to_string());
    }

    let config_path = app
        .path()
        .app_data_dir()
        .map_err(|err| err.to_string())?
        .join(DESKTOP_RUNTIME_CONFIG_FILE_NAME);
    save_desktop_runtime_config(&config_path, &DesktopRuntimeConfig { port })?;
    Ok(format!("desktop local port updated to {port}"))
}

fn html_response(body: &'static str) -> Response<Cow<'static, [u8]>> {
    Response::builder()
        .status(StatusCode::OK)
        .header(header::CONTENT_TYPE, "text/html; charset=utf-8")
        .header(header::CONTENT_SECURITY_POLICY, SERVICE_MANAGEMENT_CSP)
        .body(Cow::Borrowed(body.as_bytes()))
        .unwrap_or_else(|_| Response::new(Cow::Borrowed(b"internal server error")))
}

/// 这个协议只给服务管理窗口自己用，但同一 webview 里的任何脚本（含被注入的
/// 第三方内容）都能直接 fetch 它。自定义头不属于 CORS 简单请求：没有本应用
/// 写入的标记头就不放行，避免普通 GET/表单式请求顺带改掉端口或触发重启。
fn is_state_changing_request_authorized(method: &Method, headers: &tauri::http::HeaderMap) -> bool {
    if method == Method::GET || method == Method::HEAD {
        return true;
    }
    headers.contains_key(SERVICE_MANAGEMENT_REQUEST_HEADER)
}

fn json_response<T: Serialize>(payload: T) -> Response<Cow<'static, [u8]>> {
    match serde_json::to_vec(&payload) {
        Ok(body) => build_response(
            StatusCode::OK,
            "application/json; charset=utf-8",
            Cow::Owned(body),
        ),
        Err(err) => text_response(
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("failed to serialize json payload: {err}"),
        ),
    }
}

fn text_response(status: StatusCode, body: impl Into<String>) -> Response<Cow<'static, [u8]>> {
    build_response(
        status,
        "text/plain; charset=utf-8",
        Cow::Owned(body.into().into_bytes()),
    )
}

fn build_response(
    status: StatusCode,
    content_type: &'static str,
    body: Cow<'static, [u8]>,
) -> Response<Cow<'static, [u8]>> {
    Response::builder()
        .status(status)
        .header(header::CONTENT_TYPE, content_type)
        .body(body)
        .unwrap_or_else(|_| Response::new(Cow::Borrowed(b"internal server error")))
}

fn list_listening_processes_on_port(port: u16) -> Vec<PortProcessInfo> {
    #[cfg(target_os = "windows")]
    {
        list_listening_processes_on_port_windows(port)
    }

    #[cfg(not(target_os = "windows"))]
    {
        list_listening_processes_on_port_unix(port)
    }
}

#[cfg(not(target_os = "windows"))]
fn list_listening_processes_on_port_unix(port: u16) -> Vec<PortProcessInfo> {
    let output = Command::new("lsof")
        .args(["-nP", &format!("-iTCP:{port}"), "-sTCP:LISTEN", "-Fpcn"])
        .output();

    let Ok(output) = output else {
        return Vec::new();
    };
    if !output.status.success() {
        return Vec::new();
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    let mut results = Vec::new();
    let mut current_pid: Option<u32> = None;
    let mut current_name = String::new();
    let mut current_endpoint = String::new();

    for line in stdout.lines() {
        if let Some(rest) = line.strip_prefix('p') {
            if let Some(pid) = current_pid.take() {
                results.push(PortProcessInfo {
                    pid,
                    name: current_name.clone(),
                    endpoint: current_endpoint.clone(),
                    command: read_unix_command_line(pid),
                });
                current_name.clear();
                current_endpoint.clear();
            }
            current_pid = rest.parse().ok();
        } else if let Some(rest) = line.strip_prefix('c') {
            current_name = rest.to_string();
        } else if let Some(rest) = line.strip_prefix('n') {
            current_endpoint = rest.to_string();
        }
    }

    if let Some(pid) = current_pid {
        results.push(PortProcessInfo {
            pid,
            name: current_name,
            endpoint: current_endpoint,
            command: read_unix_command_line(pid),
        });
    }

    results
}

#[cfg(not(target_os = "windows"))]
fn read_unix_command_line(pid: u32) -> String {
    let output = Command::new("ps")
        .args(["-o", "command=", "-p", &pid.to_string()])
        .output();
    let Ok(output) = output else {
        return String::new();
    };
    String::from_utf8_lossy(&output.stdout).trim().to_string()
}

#[cfg(target_os = "windows")]
fn list_listening_processes_on_port_windows(port: u16) -> Vec<PortProcessInfo> {
    let connections = run_powershell_json(&format!(
        "Get-NetTCPConnection -State Listen -LocalPort {port} | Select-Object LocalAddress, LocalPort, OwningProcess | ConvertTo-Json -Compress"
    ));

    let Some(value) = connections else {
        return Vec::new();
    };

    let mut results = Vec::new();
    for item in as_json_items(value) {
        let Some(pid) = item
            .get("OwningProcess")
            .and_then(|value| value.as_u64())
            .map(|value| value as u32)
        else {
            continue;
        };
        let process_meta = run_powershell_json(&format!(
            "Get-CimInstance Win32_Process -Filter \"ProcessId = {pid}\" | Select-Object ProcessId, Name, ExecutablePath, CommandLine | ConvertTo-Json -Compress"
        ));

        let endpoint = format!(
            "{}:{}",
            item.get("LocalAddress")
                .and_then(|value| value.as_str())
                .unwrap_or("127.0.0.1"),
            item.get("LocalPort")
                .and_then(|value| value.as_u64())
                .unwrap_or(port as u64)
        );

        let (name, command) = if let Some(process_value) = process_meta {
            (
                process_value
                    .get("Name")
                    .and_then(|value| value.as_str())
                    .unwrap_or("")
                    .to_string(),
                process_value
                    .get("CommandLine")
                    .and_then(|value| value.as_str())
                    .or_else(|| {
                        process_value
                            .get("ExecutablePath")
                            .and_then(|value| value.as_str())
                    })
                    .unwrap_or("")
                    .to_string(),
            )
        } else {
            (String::new(), String::new())
        };

        results.push(PortProcessInfo {
            pid,
            name,
            endpoint,
            command,
        });
    }

    results
}

#[cfg(target_os = "windows")]
fn run_powershell_json(command: &str) -> Option<Value> {
    let output = Command::new("powershell")
        .args([
            "-NoProfile",
            "-NonInteractive",
            "-ExecutionPolicy",
            "Bypass",
            "-Command",
            command,
        ])
        .output()
        .ok()?;
    if !output.status.success() {
        return None;
    }
    serde_json::from_slice(&output.stdout).ok()
}

#[cfg(target_os = "windows")]
fn as_json_items(value: Value) -> Vec<Value> {
    match value {
        Value::Array(items) => items,
        Value::Null => Vec::new(),
        other => vec![other],
    }
}

#[cfg(test)]
mod tests {
    use super::is_state_changing_request_authorized;
    use tauri::http::{HeaderMap, Method};

    #[test]
    fn get_requests_are_always_authorized() {
        assert!(is_state_changing_request_authorized(
            &Method::GET,
            &HeaderMap::new()
        ));
        assert!(is_state_changing_request_authorized(
            &Method::HEAD,
            &HeaderMap::new()
        ));
    }

    #[test]
    fn state_changing_requests_without_the_marker_are_rejected() {
        for method in [Method::POST, Method::PUT, Method::DELETE] {
            assert!(
                !is_state_changing_request_authorized(&method, &HeaderMap::new()),
                "{method} must require the marker header"
            );
        }
    }

    #[test]
    fn state_changing_requests_with_the_marker_are_authorized() {
        let mut headers = HeaderMap::new();
        headers.insert("x-newapi-service-request", "1".parse().unwrap());
        assert!(is_state_changing_request_authorized(
            &Method::POST,
            &headers
        ));
    }
}
