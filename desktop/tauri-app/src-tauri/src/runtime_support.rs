use std::{
    fs,
    path::{Path, PathBuf},
};

use serde::{Deserialize, Serialize};

use crate::constants::DEFAULT_LOCAL_SERVER_PORT;

const DESKTOP_SECRET_BYTES: usize = 32;
const DESKTOP_SECRET_PLACEHOLDER: &str = "random_string";
const DESKTOP_SECRET_FILE_VERSION: u32 = 1;
const DESKTOP_RUNTIME_CONFIG_FILE_VERSION: u32 = 1;
const PROBE_BODY_SNIPPET_LIMIT: usize = 120;

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct ErrorDiagnosis {
    pub title: String,
    pub summary: String,
    pub detail: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DesktopSecrets {
    pub session_secret: String,
    pub crypto_secret: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DesktopRuntimeConfig {
    pub port: u16,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ReadinessProbeResult {
    Ready,
    NotReady { observation: String },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SingleInstanceFocusTarget {
    MainWindow,
    ServiceManagementWindow,
    None,
}

#[derive(Debug, Serialize, Deserialize)]
struct PersistedDesktopSecrets {
    version: u32,
    session_secret: String,
    crypto_secret: String,
}

#[derive(Debug, Serialize, Deserialize)]
struct PersistedDesktopRuntimeConfig {
    version: u32,
    port: u16,
}

pub fn load_or_create_desktop_secrets(secret_file: &Path) -> Result<DesktopSecrets, String> {
    if secret_file.exists() {
        return load_existing_desktop_secrets(secret_file);
    }

    let secrets = DesktopSecrets {
        session_secret: generate_secret()?,
        crypto_secret: generate_secret()?,
    };
    persist_desktop_secrets(secret_file, &secrets)?;
    Ok(secrets)
}

pub fn load_or_create_desktop_runtime_config(
    config_file: &Path,
) -> Result<DesktopRuntimeConfig, String> {
    if config_file.exists() {
        return load_existing_desktop_runtime_config(config_file);
    }

    if let Ok(env_port) = std::env::var("NEW_API_DESKTOP_PORT") {
        let config = DesktopRuntimeConfig {
            port: validate_local_server_port(&env_port)?,
        };
        persist_desktop_runtime_config(config_file, &config)?;
        return Ok(config);
    }

    let config = DesktopRuntimeConfig {
        port: DEFAULT_LOCAL_SERVER_PORT,
    };
    persist_desktop_runtime_config(config_file, &config)?;
    Ok(config)
}

pub fn save_desktop_runtime_config(
    config_file: &Path,
    config: &DesktopRuntimeConfig,
) -> Result<(), String> {
    if config.port == 0 {
        return Err(format!(
            "desktop runtime config is invalid: {}",
            config_file.display()
        ));
    }
    persist_desktop_runtime_config(config_file, config)
}

pub fn is_ready_status_response(response: &str) -> bool {
    let mut parts = response.splitn(2, "\r\n\r\n");
    let header = parts.next().unwrap_or_default();
    let raw_body = parts.next().unwrap_or_default();

    if !(header.starts_with("HTTP/1.1 200") || header.starts_with("HTTP/1.0 200")) {
        return false;
    }

    // 探针走裸 socket，读到的是未解帧的原始报文。/api/status 的响应体
    // 超过 Go 的 bufferBeforeChunkingSize（2048 字节），服务器不下发
    // Content-Length 而改用 Transfer-Encoding: chunked，body 前面就多出
    // 十六进制长度行。直接把它当 JSON 解析必然失败，桌面端会误判成
    // "另一个本地服务占用了端口"。
    let body = if header_has_chunked_transfer_encoding(header) {
        match decode_chunked_body(raw_body) {
            Some(decoded) => decoded,
            None => return false,
        }
    } else {
        raw_body.to_string()
    };

    let payload: serde_json::Value = match serde_json::from_str(&body) {
        Ok(value) => value,
        Err(_) => return false,
    };
    if payload.get("success").and_then(serde_json::Value::as_bool) != Some(true) {
        return false;
    }
    // /api/status 把版本号包在 data 里，顶层没有 version。
    payload
        .get("data")
        .and_then(|data| data.get("version"))
        .and_then(serde_json::Value::as_str)
        .is_some_and(|version| !version.is_empty())
}

fn header_has_chunked_transfer_encoding(header: &str) -> bool {
    header.lines().skip(1).any(|line| {
        let Some((name, value)) = line.split_once(':') else {
            return false;
        };
        name.trim().eq_ignore_ascii_case("transfer-encoding")
            && value
                .split(',')
                .any(|token| token.trim().eq_ignore_ascii_case("chunked"))
    })
}

// 按 RFC 9112 拆 chunk：每个 chunk 是 "十六进制长度[;扩展]\r\n数据\r\n"，
// 以长度为 0 的 chunk 结束。缺少终止 chunk 说明响应被截断，按未就绪处理，
// 不能把半截 JSON 当成有效状态。chunk 扩展（分号之后）一律忽略。
// 在字节层拼接：chunk 边界可以切在多字节 UTF-8 字符中间。
fn decode_chunked_body(raw: &str) -> Option<String> {
    let mut decoded: Vec<u8> = Vec::new();
    let mut rest = raw.as_bytes();
    loop {
        let newline = find_crlf(rest)?;
        let size_line = std::str::from_utf8(&rest[..newline]).ok()?;
        let size_token = size_line.split(';').next().unwrap_or_default().trim();
        let size = usize::from_str_radix(size_token, 16).ok()?;
        if size == 0 {
            return String::from_utf8(decoded).ok();
        }
        let data_start = newline + 2;
        let data_end = data_start.checked_add(size)?;
        if rest.len() < data_end {
            return None;
        }
        decoded.extend_from_slice(&rest[data_start..data_end]);
        rest = rest[data_end..].strip_prefix(b"\r\n")?;
    }
}

fn find_crlf(bytes: &[u8]) -> Option<usize> {
    bytes.windows(2).position(|pair| pair == b"\r\n")
}

pub fn analyze_startup_error(detail: &str, log_lines: &[String]) -> ErrorDiagnosis {
    let joined_logs = if log_lines.is_empty() {
        detail.to_string()
    } else {
        format!("{detail}\n{}", log_lines.join("\n"))
    };
    let configured_port = extract_port_from_text(&joined_logs).unwrap_or(DEFAULT_LOCAL_SERVER_PORT);
    let joined_logs_lower = joined_logs.to_lowercase();

    if joined_logs.contains("bind: address already in use")
        || joined_logs.contains("failed to start HTTP server")
        || joined_logs.contains("is already in use before starting desktop sidecar")
    {
        return ErrorDiagnosis {
            title: format!("Port {configured_port} is already in use"),
            summary: format!(
                "New API could not start because port {configured_port} is already occupied."
            ),
            detail: format!(
                "Close the conflicting process, stop the other New API instance, or change the desktop local port from {configured_port}."
            ),
        };
    }

    if joined_logs.contains("failed to parse desktop runtime config")
        || joined_logs.contains("desktop runtime config is invalid")
        || joined_logs.contains("invalid NEW_API_DESKTOP_PORT")
    {
        return ErrorDiagnosis {
            title: "Desktop runtime config is invalid".to_string(),
            summary: "The desktop local runtime configuration could not be loaded."
                .to_string(),
            detail: "Open Service Management, choose a valid local port, save the configuration and retry startup."
                .to_string(),
        };
    }

    if joined_logs.contains("failed to parse desktop secret file")
        || joined_logs.contains("desktop secret file is invalid")
        || joined_logs.contains("failed to read desktop secret file")
    {
        // 这个文件里的 CRYPTO_SECRET 加密着已保存的渠道密钥。静默重新生成
        // 会让这些数据永久不可解，所以这里只能显式报错并说明恢复手段。
        return ErrorDiagnosis {
            title: "Desktop secret file is invalid".to_string(),
            summary: "The desktop secret file (desktop-secrets.json) could not be read or parsed."
                .to_string(),
            detail: "Restore desktop-secrets.json from a backup, or delete it to let the app generate new secrets. Deleting it changes SESSION_SECRET (all local sessions are signed out) and CRYPTO_SECRET (channel credentials encrypted with the old value can no longer be decrypted)."
                .to_string(),
        };
    }

    if joined_logs_lower.contains("database is locked")
        || joined_logs_lower.contains("unable to open database")
    {
        return ErrorDiagnosis {
            title: "Database file is locked".to_string(),
            summary: "The local SQLite data file is being used by another process.".to_string(),
            detail: "Ensure only one desktop instance is using the data directory, then retry."
                .to_string(),
        };
    }

    if joined_logs_lower.contains("permission denied")
        || joined_logs_lower.contains("access denied")
    {
        return ErrorDiagnosis {
            title: "Permission denied".to_string(),
            summary: "New API does not have permission to access a required file or directory."
                .to_string(),
            detail:
                "Check file permissions for the application bundle, data directory and log directory."
                    .to_string(),
        };
    }

    if joined_logs.contains("timed out waiting for ready local server") {
        if let Some(observation) = extract_last_probe_observation(&joined_logs) {
            if observation.starts_with("HTTP/1.1")
                || observation.starts_with("HTTP/1.0")
                || observation.contains("status=")
            {
                return ErrorDiagnosis {
                    title: format!("Unexpected service responded on port {configured_port}"),
                    summary: format!(
                        "Port {configured_port} answered the readiness probe, but the response did not look like New API."
                    ),
                    detail: format!(
                        "Another local service may already be listening on port {configured_port}. Last probe observation: {observation}"
                    ),
                };
            }
        }

        return ErrorDiagnosis {
            title: "Local server did not become ready".to_string(),
            summary: format!(
                "New API opened port {configured_port} but did not pass the readiness check in time."
            ),
            detail: format!(
                "Check the sidecar logs for startup failures, database migration problems or another process responding on port {configured_port}."
            ),
        };
    }

    if joined_logs.contains("no such file or directory") {
        return ErrorDiagnosis {
            title: "Required file is missing".to_string(),
            summary: "A required runtime file could not be found.".to_string(),
            detail:
                "Rebuild the desktop bundle and verify the sidecar binary is packaged correctly."
                    .to_string(),
        };
    }

    ErrorDiagnosis {
        title: "New API startup error".to_string(),
        summary: "The desktop runtime failed before the local service became stable.".to_string(),
        detail: "Use \"View Log\" to inspect the captured sidecar output and startup trace."
            .to_string(),
    }
}

pub fn is_service_management_recoverable_error(diagnosis: &ErrorDiagnosis) -> bool {
    diagnosis.title.starts_with("Port ")
        || diagnosis.title.starts_with("Unexpected service")
        || diagnosis.title == "Desktop runtime config is invalid"
}

pub fn resolve_single_instance_focus_target(
    has_main_window: bool,
    has_service_management_window: bool,
) -> SingleInstanceFocusTarget {
    if has_main_window {
        SingleInstanceFocusTarget::MainWindow
    } else if has_service_management_window {
        SingleInstanceFocusTarget::ServiceManagementWindow
    } else {
        SingleInstanceFocusTarget::None
    }
}

pub fn probe_readiness_response(response: &str) -> ReadinessProbeResult {
    if is_ready_status_response(response) {
        ReadinessProbeResult::Ready
    } else {
        ReadinessProbeResult::NotReady {
            observation: summarize_probe_response(response),
        }
    }
}

fn load_existing_desktop_secrets(secret_file: &Path) -> Result<DesktopSecrets, String> {
    let persisted = fs::read_to_string(secret_file)
        .map_err(|err| format!("failed to read desktop secret file: {err}"))?;
    let raw: PersistedDesktopSecrets = serde_json::from_str(&persisted)
        .map_err(|err| format!("failed to parse desktop secret file: {err}"))?;

    let session_secret = normalize_secret_value(&raw.session_secret);
    let crypto_secret = normalize_secret_value(&raw.crypto_secret);

    match (session_secret, crypto_secret) {
        (Some(session_secret), Some(crypto_secret)) => Ok(DesktopSecrets {
            session_secret,
            crypto_secret,
        }),
        _ => Err(format!(
            "desktop secret file is invalid: {}",
            secret_file.display()
        )),
    }
}

fn load_existing_desktop_runtime_config(
    config_file: &Path,
) -> Result<DesktopRuntimeConfig, String> {
    let persisted = fs::read_to_string(config_file)
        .map_err(|err| format!("failed to read desktop runtime config: {err}"))?;
    let raw: PersistedDesktopRuntimeConfig = serde_json::from_str(&persisted)
        .map_err(|err| format!("failed to parse desktop runtime config: {err}"))?;

    if raw.port == 0 {
        return Err(format!(
            "desktop runtime config is invalid: {}",
            config_file.display()
        ));
    }

    Ok(DesktopRuntimeConfig { port: raw.port })
}

fn persist_desktop_secrets(secret_file: &Path, secrets: &DesktopSecrets) -> Result<(), String> {
    let parent_dir = secret_file.parent().ok_or_else(|| {
        format!(
            "desktop secret file must have a parent directory: {}",
            secret_file.display()
        )
    })?;
    fs::create_dir_all(parent_dir)
        .map_err(|err| format!("failed to create desktop secret directory: {err}"))?;
    restrict_directory_permissions(parent_dir)?;

    let payload = PersistedDesktopSecrets {
        version: DESKTOP_SECRET_FILE_VERSION,
        session_secret: secrets.session_secret.clone(),
        crypto_secret: secrets.crypto_secret.clone(),
    };
    let serialized = serde_json::to_vec_pretty(&payload)
        .map_err(|err| format!("failed to serialize desktop secrets: {err}"))?;

    let temp_path = temp_secret_file_path(secret_file);
    write_private_file(&temp_path, &serialized)
        .map_err(|err| format!("failed to write desktop secret temp file: {err}"))?;
    fs::rename(&temp_path, secret_file)
        .map_err(|err| format!("failed to finalize desktop secret file: {err}"))?;
    Ok(())
}

/// Write `contents` to `path` so the file is only readable by its owner and
/// the bytes are on disk before a rename makes them visible.
///
/// The default umask (022) makes a freshly written file world-readable, and a
/// plain write leaves it in the page cache: a crash right after the rename can
/// leave a zero-length secrets file, which costs the user the CRYPTO_SECRET.
fn write_private_file(path: &Path, contents: &[u8]) -> std::io::Result<()> {
    use std::io::Write;

    let mut options = fs::OpenOptions::new();
    options.write(true).create(true).truncate(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        // mode() only applies when the file is created; an existing temp file
        // from an interrupted write keeps its old mode, so set it again below.
        options.mode(0o600);
    }
    let mut file = options.open(path)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        file.set_permissions(fs::Permissions::from_mode(0o600))?;
    }
    file.write_all(contents)?;
    file.sync_all()?;
    Ok(())
}

#[cfg(unix)]
fn restrict_directory_permissions(dir: &Path) -> Result<(), String> {
    use std::os::unix::fs::PermissionsExt;
    fs::set_permissions(dir, fs::Permissions::from_mode(0o700))
        .map_err(|err| format!("failed to restrict desktop data directory permissions: {err}"))
}

#[cfg(not(unix))]
fn restrict_directory_permissions(_dir: &Path) -> Result<(), String> {
    Ok(())
}

fn persist_desktop_runtime_config(
    config_file: &Path,
    config: &DesktopRuntimeConfig,
) -> Result<(), String> {
    let parent_dir = config_file.parent().ok_or_else(|| {
        format!(
            "desktop runtime config must have a parent directory: {}",
            config_file.display()
        )
    })?;
    fs::create_dir_all(parent_dir)
        .map_err(|err| format!("failed to create desktop runtime config directory: {err}"))?;

    let payload = PersistedDesktopRuntimeConfig {
        version: DESKTOP_RUNTIME_CONFIG_FILE_VERSION,
        port: config.port,
    };
    let serialized = serde_json::to_vec_pretty(&payload)
        .map_err(|err| format!("failed to serialize desktop runtime config: {err}"))?;

    let temp_path = temp_config_file_path(config_file);
    fs::write(&temp_path, serialized)
        .map_err(|err| format!("failed to write desktop runtime config temp file: {err}"))?;
    fs::rename(&temp_path, config_file)
        .map_err(|err| format!("failed to finalize desktop runtime config file: {err}"))?;
    Ok(())
}

fn extract_last_probe_observation(input: &str) -> Option<String> {
    let marker = "last probe observation: ";
    let line = input
        .lines()
        .rev()
        .find(|line| line.contains(marker))?
        .trim();
    let index = line.find(marker)?;
    Some(line[(index + marker.len())..].trim().to_string())
}

fn extract_port_from_text(input: &str) -> Option<u16> {
    let marker = "127.0.0.1:";
    let start = input.find(marker)? + marker.len();
    let digits: String = input[start..]
        .chars()
        .take_while(|char| char.is_ascii_digit())
        .collect();
    if digits.is_empty() {
        return None;
    }
    digits.parse().ok()
}

fn temp_secret_file_path(secret_file: &Path) -> PathBuf {
    let file_name = secret_file
        .file_name()
        .map(|value| value.to_string_lossy().to_string())
        .unwrap_or_else(|| "desktop-secrets.json".to_string());
    secret_file.with_file_name(format!("{file_name}.tmp"))
}

fn temp_config_file_path(config_file: &Path) -> PathBuf {
    let file_name = config_file
        .file_name()
        .map(|value| value.to_string_lossy().to_string())
        .unwrap_or_else(|| "desktop-runtime.json".to_string());
    config_file.with_file_name(format!("{file_name}.tmp"))
}

fn normalize_secret_value(value: &str) -> Option<String> {
    let trimmed = value.trim();
    if trimmed.is_empty() || trimmed == DESKTOP_SECRET_PLACEHOLDER {
        return None;
    }
    Some(trimmed.to_string())
}

fn validate_local_server_port(input: &str) -> Result<u16, String> {
    let port: u16 = input
        .trim()
        .parse()
        .map_err(|_| format!("invalid NEW_API_DESKTOP_PORT: {input}"))?;
    if port == 0 {
        return Err("invalid NEW_API_DESKTOP_PORT: 0".to_string());
    }
    Ok(port)
}

fn generate_secret() -> Result<String, String> {
    let mut bytes = [0_u8; DESKTOP_SECRET_BYTES];
    getrandom::fill(&mut bytes)
        .map_err(|err| format!("failed to generate desktop secret: {err}"))?;
    Ok(hex_encode(&bytes))
}

fn summarize_probe_response(response: &str) -> String {
    let mut parts = response.splitn(2, "\r\n\r\n");
    let header = parts.next().unwrap_or_default();
    let body = parts.next().unwrap_or_default();
    let status_line = header.lines().next().unwrap_or("empty response").trim();
    let body_prefix = normalize_probe_body_prefix(body);

    if body_prefix.is_empty() {
        status_line.to_string()
    } else {
        format!("status={status_line}; body_prefix={body_prefix}")
    }
}

fn normalize_probe_body_prefix(body: &str) -> String {
    let normalized = body.split_whitespace().collect::<Vec<_>>().join(" ");
    if normalized.is_empty() {
        return String::new();
    }

    normalized.chars().take(PROBE_BODY_SNIPPET_LIMIT).collect()
}

fn hex_encode(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        output.push(HEX[(byte >> 4) as usize] as char);
        output.push(HEX[(byte & 0x0f) as usize] as char);
    }
    output
}
