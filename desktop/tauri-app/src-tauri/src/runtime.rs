use std::{
    fs,
    io::{ErrorKind, Read, Write},
    net::{SocketAddr, TcpStream},
    path::{Path, PathBuf},
    thread,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use tauri::{AppHandle, Manager, Runtime};
use tauri_plugin_dialog::{
    DialogExt, MessageDialogButtons, MessageDialogKind, MessageDialogResult,
};
use tauri_plugin_shell::{
    process::{CommandChild, CommandEvent},
    ShellExt,
};

use crate::{
    constants::{
        APP_NAME, DEFAULT_LOCAL_SERVER_HOST, DESKTOP_RUNTIME_CONFIG_FILE_NAME,
        DESKTOP_SECRET_FILE_NAME, LOCAL_SERVER_STATUS_PATH, SQLITE_FILE_NAME, STARTUP_DELAY,
        STARTUP_RETRIES,
    },
    runtime_support::{
        analyze_startup_error, is_service_management_recoverable_error,
        load_or_create_desktop_runtime_config, load_or_create_desktop_secrets,
        probe_readiness_response, DesktopRuntimeConfig, ReadinessProbeResult,
    },
    state::{
        clear_sidecar, clear_starting, clear_startup_error, clear_stopping_sidecar,
        has_running_sidecar, is_quitting, is_starting, is_stopping_sidecar, mark_quitting,
        mark_stopping_sidecar, push_error_log, set_startup_error, snapshot_error_logs,
        snapshot_startup_error, try_mark_starting, DesktopState,
    },
    windowing::{
        close_service_management_window, create_main_window, open_service_management_window,
    },
};

pub fn spawn_bootstrap_desktop_runtime<R: Runtime>(app: AppHandle<R>) {
    if !try_mark_starting(&app) {
        return;
    }

    thread::spawn(move || {
        let result = bootstrap_desktop_runtime(app.clone());
        clear_starting(&app);

        match result {
            Ok(()) => {
                clear_startup_error(&app);
                close_service_management_window(&app);
            }
            Err(err) => handle_bootstrap_failure(&app, err),
        }
    });
}

pub fn request_startup_retry<R: Runtime>(app: &AppHandle<R>) -> Result<String, String> {
    if has_running_sidecar(app) {
        return Err("desktop sidecar is already running".to_string());
    }
    if is_starting(app) {
        return Err("desktop sidecar startup is already in progress".to_string());
    }

    clear_startup_error(app);
    spawn_bootstrap_desktop_runtime(app.clone());
    Ok("desktop startup retry requested".to_string())
}

pub fn bootstrap_desktop_runtime<R: Runtime>(app: AppHandle<R>) -> Result<(), String> {
    clear_stopping_sidecar(&app);
    let app_data_dir = resolve_app_data_dir(&app).map_err(|err| err.to_string())?;
    let data_dir = resolve_data_dir(&app_data_dir);
    fs::create_dir_all(&data_dir).map_err(|err| err.to_string())?;
    let runtime_config = load_or_create_desktop_runtime_config(
        &app_data_dir.join(DESKTOP_RUNTIME_CONFIG_FILE_NAME),
    )?;
    let desktop_secrets =
        load_or_create_desktop_secrets(&app_data_dir.join(DESKTOP_SECRET_FILE_NAME))?;
    let local_server_url = format_local_server_url(&runtime_config);

    ensure_local_server_port_available(&runtime_config)?;

    let sqlite_path = data_dir.join(SQLITE_FILE_NAME);
    let sidecar_command = app
        .shell()
        .sidecar("new-api")
        .map_err(|err| err.to_string())?
        .current_dir(&app_data_dir)
        .env("PORT", runtime_config.port.to_string())
        .env("NEW_API_SKIP_DOTENV", "true")
        .env("SQL_DSN", "local")
        .env("LOG_SQL_DSN", "")
        .env("REDIS_CONN_STRING", "")
        .env("SESSION_SECRET", desktop_secrets.session_secret)
        .env("CRYPTO_SECRET", desktop_secrets.crypto_secret)
        .env("SQLITE_PATH", sqlite_path.to_string_lossy().to_string());

    let (rx, child) = sidecar_command.spawn().map_err(|err| err.to_string())?;
    store_sidecar(&app, child);

    let sidecar_monitor_handle = app.clone();
    thread::spawn(move || {
        let mut receiver = rx;
        while let Some(event) = receiver.blocking_recv() {
            match event {
                CommandEvent::Stdout(line) => {
                    println!(
                        "sidecar stdout: {}",
                        String::from_utf8_lossy(&line).trim_end()
                    );
                }
                CommandEvent::Stderr(line) => {
                    let message = String::from_utf8_lossy(&line).trim_end().to_string();
                    eprintln!("sidecar stderr: {message}");
                    push_error_log(&sidecar_monitor_handle, format!("stderr: {message}"));
                }
                CommandEvent::Error(message) => {
                    eprintln!("sidecar error: {message}");
                    push_error_log(&sidecar_monitor_handle, format!("sidecar error: {message}"));
                }
                CommandEvent::Terminated(payload) => {
                    clear_sidecar(&sidecar_monitor_handle);

                    let exit_code = payload
                        .code
                        .map(|code| code.to_string())
                        .unwrap_or_else(|| "unknown".to_string());
                    let signal = payload
                        .signal
                        .map(|item| item.to_string())
                        .unwrap_or_else(|| "none".to_string());
                    let termination_message =
                        format!("sidecar terminated: code={exit_code} signal={signal}");
                    eprintln!("{termination_message}");
                    push_error_log(&sidecar_monitor_handle, termination_message.clone());

                    if !is_quitting(&sidecar_monitor_handle)
                        && !is_stopping_sidecar(&sidecar_monitor_handle)
                    {
                        report_fatal_error(
                            &sidecar_monitor_handle,
                            format!(
                                "sidecar terminated unexpectedly (code={exit_code}, signal={signal})"
                            ),
                        );
                    }
                    clear_stopping_sidecar(&sidecar_monitor_handle);
                }
                _ => {}
            }
        }
    });

    wait_for_local_server(&runtime_config)?;
    create_main_window(app, data_dir, local_server_url)
}

fn handle_bootstrap_failure<R: Runtime>(app: &AppHandle<R>, err: String) {
    let detail = format!("failed to bootstrap desktop runtime: {err}");
    stop_sidecar(app);
    set_startup_error(app, detail.clone());
    push_error_log(app, detail.clone());

    let diagnosis = analyze_startup_error(&detail, &snapshot_error_logs(app));
    if is_service_management_recoverable_error(&diagnosis) {
        if let Err(window_err) = open_service_management_window(app) {
            report_fatal_error(
                app,
                format!("{detail}\nfailed to open service management window: {window_err}"),
            );
        }
        return;
    }

    report_fatal_error(app, detail);
}

pub fn report_fatal_error<R: Runtime>(app: &AppHandle<R>, detail: String) {
    let state = app.state::<DesktopState>();
    if state
        .fatal_error_reported
        .swap(true, std::sync::atomic::Ordering::SeqCst)
    {
        return;
    }

    eprintln!("{detail}");
    let should_append_error_log = snapshot_startup_error(app).as_deref() != Some(detail.as_str());
    set_startup_error(app, detail.clone());
    if should_append_error_log {
        push_error_log(app, detail.clone());
    }
    let diagnosis = analyze_startup_error(&detail, &snapshot_error_logs(app));
    let app_handle = app.clone();
    app.dialog()
        .message(format!("{}\n\n{}", diagnosis.summary, diagnosis.detail))
        .title(diagnosis.title)
        .kind(MessageDialogKind::Error)
        .buttons(MessageDialogButtons::OkCancelCustom(
            "View Log".to_string(),
            "Exit".to_string(),
        ))
        .show_with_result(move |result| {
            if matches!(
                result,
                MessageDialogResult::Ok | MessageDialogResult::Custom(_)
            ) {
                if let Some(log_path) = persist_error_log(&app_handle) {
                    open_log_file(&app_handle, &log_path);
                }
            }
            mark_quitting(&app_handle);
            app_handle.exit(1);
        });
}

pub fn stop_sidecar<R: Runtime>(app_handle: &AppHandle<R>) {
    mark_stopping_sidecar(app_handle);
    let sidecar_state = app_handle.state::<DesktopState>();
    let mut guard = sidecar_state
        .sidecar
        .lock()
        .expect("sidecar state lock poisoned");

    if let Some(child) = guard.take() {
        if let Err(err) = child.kill() {
            eprintln!("failed to stop sidecar: {err}");
            clear_stopping_sidecar(app_handle);
        }
    } else {
        clear_stopping_sidecar(app_handle);
    }
    clear_starting(app_handle);
}

fn resolve_app_data_dir<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<PathBuf> {
    app.path().app_data_dir()
}

fn resolve_data_dir(app_data_dir: &Path) -> PathBuf {
    app_data_dir.join("data")
}

fn wait_for_local_server(runtime_config: &DesktopRuntimeConfig) -> Result<(), String> {
    let address = parse_local_server_address(runtime_config)?;
    let mut last_observation: Option<String> = None;

    for _ in 0..STARTUP_RETRIES {
        match probe_local_server_ready(&address) {
            Ok(ReadinessProbeResult::Ready) => return Ok(()),
            Ok(ReadinessProbeResult::NotReady { observation }) => {
                eprintln!("local server is not ready yet: {observation}");
                last_observation = Some(observation);
            }
            Err(err) => {
                eprintln!("local server readiness probe failed: {err}");
                last_observation = Some(format!("probe error: {err}"));
            }
        }
        thread::sleep(STARTUP_DELAY);
    }

    let detail = if let Some(observation) = last_observation {
        format!("; last probe observation: {observation}")
    } else {
        String::new()
    };

    Err(format!(
        "timed out waiting for ready local server at {DEFAULT_LOCAL_SERVER_HOST}:{}{LOCAL_SERVER_STATUS_PATH}{detail}",
        runtime_config.port
    ))
}

fn ensure_local_server_port_available(runtime_config: &DesktopRuntimeConfig) -> Result<(), String> {
    let address = parse_local_server_address(runtime_config)?;
    let local_server_address = format_local_server_address(runtime_config);
    match TcpStream::connect_timeout(&address, Duration::from_millis(300)) {
        Ok(_) => Err(format!(
            "port {local_server_address} is already in use before starting desktop sidecar"
        )),
        Err(err) if err.kind() == ErrorKind::ConnectionRefused => Ok(()),
        Err(err) => Err(format!(
            "could not confirm port {local_server_address} is available before starting desktop sidecar: {err}"
        )),
    }
}

fn parse_local_server_address(runtime_config: &DesktopRuntimeConfig) -> Result<SocketAddr, String> {
    format_local_server_address(runtime_config)
        .parse()
        .map_err(|err: std::net::AddrParseError| err.to_string())
}

fn format_local_server_address(runtime_config: &DesktopRuntimeConfig) -> String {
    format!("{DEFAULT_LOCAL_SERVER_HOST}:{}", runtime_config.port)
}

fn format_local_server_url(runtime_config: &DesktopRuntimeConfig) -> String {
    format!(
        "http://{}:{}",
        DEFAULT_LOCAL_SERVER_HOST, runtime_config.port
    )
}

fn probe_local_server_ready(address: &SocketAddr) -> Result<ReadinessProbeResult, String> {
    let mut stream = TcpStream::connect_timeout(address, Duration::from_secs(2))
        .map_err(|err| err.to_string())?;
    stream
        .set_read_timeout(Some(Duration::from_secs(2)))
        .map_err(|err| err.to_string())?;
    stream
        .set_write_timeout(Some(Duration::from_secs(2)))
        .map_err(|err| err.to_string())?;

    let request = format!(
        "GET {LOCAL_SERVER_STATUS_PATH} HTTP/1.1\r\nHost: {address}\r\nConnection: close\r\n\r\n"
    );
    stream
        .write_all(request.as_bytes())
        .map_err(|err| err.to_string())?;

    let mut response = String::new();
    stream
        .read_to_string(&mut response)
        .map_err(|err| err.to_string())?;

    Ok(probe_readiness_response(&response))
}

fn store_sidecar<R: Runtime>(app: &AppHandle<R>, child: CommandChild) {
    let sidecar_state = app.state::<DesktopState>();
    let mut guard = sidecar_state
        .sidecar
        .lock()
        .expect("sidecar state lock poisoned");
    *guard = Some(child);
}

fn persist_error_log<R: Runtime>(app: &AppHandle<R>) -> Option<PathBuf> {
    let log_dir = app.path().app_log_dir().ok()?;
    fs::create_dir_all(&log_dir).ok()?;

    let timestamp = SystemTime::now().duration_since(UNIX_EPOCH).ok()?.as_secs();
    let log_path = log_dir.join(format!("new-api-crash-{timestamp}.log"));
    let content = build_error_log_content(app, &log_path);
    fs::write(&log_path, content).ok()?;
    Some(log_path)
}

fn build_error_log_content<R: Runtime>(app: &AppHandle<R>, log_path: &Path) -> String {
    let package_info = app.package_info();
    let log_lines = snapshot_error_logs(app);
    let timestamp = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs().to_string())
        .unwrap_or_else(|_| "unknown".to_string());

    format!(
        "{APP_NAME} crash log\ncreated_at_unix={timestamp}\nplatform={}\narch={}\napp_version={}\nlog_path={}\n\ncaptured_logs:\n{}\n",
        std::env::consts::OS,
        std::env::consts::ARCH,
        package_info.version,
        log_path.display(),
        if log_lines.is_empty() {
            "no captured logs".to_string()
        } else {
            log_lines.join("\n")
        }
    )
}

fn open_log_file<R: Runtime>(app: &AppHandle<R>, log_path: &Path) {
    #[allow(deprecated)]
    if let Err(err) = app
        .shell()
        .open(log_path.to_string_lossy().to_string(), None)
    {
        eprintln!("failed to open crash log: {err}");
    }
}
