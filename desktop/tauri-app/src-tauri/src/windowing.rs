use std::{
    fs,
    path::{Path, PathBuf},
    sync::mpsc,
};

use tauri::{
    menu::{Menu, MenuItemBuilder},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Manager, Runtime, WebviewUrl, WebviewWindow, WebviewWindowBuilder, Window,
    WindowEvent,
};
use tauri_plugin_shell::ShellExt;

use crate::{
    constants::{
        APP_NAME, SERVICE_MANAGEMENT_PROTOCOL, SERVICE_MANAGEMENT_URL,
        SERVICE_MANAGEMENT_WINDOW_LABEL, SERVICE_MANAGEMENT_WINDOW_TITLE, TRAY_ID,
        TRAY_MENU_OPEN_DATA_DIR_ID, TRAY_MENU_OPEN_LOG_DIR_ID, TRAY_MENU_QUIT_ID,
        TRAY_MENU_SERVICE_MANAGEMENT_ID, TRAY_MENU_SHOW_ID, WINDOW_LABEL, WINDOW_TITLE,
    },
    state::{is_quitting, mark_quitting},
};

pub fn create_main_window<R: Runtime>(
    app: AppHandle<R>,
    data_dir: PathBuf,
    local_server_url: String,
) -> Result<(), String> {
    let runtime_script = build_runtime_script(&data_dir);
    let app_handle = app.clone();
    let (tx, rx) = mpsc::channel();

    app.run_on_main_thread(move || {
        let result = if app_handle.get_webview_window(WINDOW_LABEL).is_some() {
            Ok(())
        } else {
            WebviewWindowBuilder::new(
                &app_handle,
                WINDOW_LABEL,
                WebviewUrl::External(local_server_url.parse().expect("invalid local server url")),
            )
            .title(WINDOW_TITLE)
            .inner_size(1080.0, 720.0)
            .initialization_script(&runtime_script)
            .build()
            .map(|_| ())
            .map_err(|err| err.to_string())
        };

        let _ = tx.send(result);
    })
    .map_err(|err| err.to_string())?;

    rx.recv().map_err(|err| err.to_string())?
}

pub fn create_tray(app: &AppHandle) -> tauri::Result<()> {
    let show_item = MenuItemBuilder::with_id(TRAY_MENU_SHOW_ID, "Show New API").build(app)?;
    let service_management_item =
        MenuItemBuilder::with_id(TRAY_MENU_SERVICE_MANAGEMENT_ID, "Service Management")
            .build(app)?;
    let data_dir_item =
        MenuItemBuilder::with_id(TRAY_MENU_OPEN_DATA_DIR_ID, "Open Data Directory").build(app)?;
    let log_dir_item =
        MenuItemBuilder::with_id(TRAY_MENU_OPEN_LOG_DIR_ID, "Open Log Directory").build(app)?;
    let quit_item = MenuItemBuilder::with_id(TRAY_MENU_QUIT_ID, "Quit").build(app)?;

    let menu = Menu::new(app)?;
    menu.append(&show_item)?;
    menu.append(&service_management_item)?;
    menu.append(&data_dir_item)?;
    menu.append(&log_dir_item)?;
    menu.append(&quit_item)?;

    let mut tray_builder = TrayIconBuilder::with_id(TRAY_ID)
        .menu(&menu)
        .tooltip(APP_NAME)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id().as_ref() {
            TRAY_MENU_SHOW_ID => {
                let _ = show_main_window(app);
            }
            TRAY_MENU_SERVICE_MANAGEMENT_ID => {
                let _ = open_service_management_window(app);
            }
            TRAY_MENU_OPEN_DATA_DIR_ID => {
                if let Err(err) = open_data_dir(app) {
                    eprintln!("failed to open data directory: {err}");
                }
            }
            TRAY_MENU_OPEN_LOG_DIR_ID => {
                if let Err(err) = open_log_dir(app) {
                    eprintln!("failed to open log directory: {err}");
                }
            }
            TRAY_MENU_QUIT_ID => {
                mark_quitting(app);
                app.exit(0);
            }
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                let _ = toggle_main_window(tray.app_handle());
            }
        });

    if let Some(icon) = app.default_window_icon().cloned() {
        tray_builder = tray_builder.icon(icon);
    }

    tray_builder.build(app)?;
    Ok(())
}

pub fn handle_window_event(window: &Window, event: &WindowEvent) {
    if window.label() != WINDOW_LABEL {
        return;
    }

    if let WindowEvent::CloseRequested { api, .. } = event {
        if is_quitting(window.app_handle()) {
            return;
        }

        api.prevent_close();
        if let Err(err) = hide_main_window(window.app_handle()) {
            eprintln!("failed to hide main window on close request: {err}");
        }
    }
}

pub fn open_service_management_window<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    let app_handle = app.clone();
    let (tx, rx) = mpsc::channel();

    app.run_on_main_thread(move || {
        let result = (|| {
            if let Some(window) = app_handle.get_webview_window(SERVICE_MANAGEMENT_WINDOW_LABEL) {
                return show_window(&app_handle, &window);
            }

            let service_url = SERVICE_MANAGEMENT_URL
                .parse()
                .expect("invalid service management url");
            let window = WebviewWindowBuilder::new(
                &app_handle,
                SERVICE_MANAGEMENT_WINDOW_LABEL,
                WebviewUrl::CustomProtocol(service_url),
            )
            .title(SERVICE_MANAGEMENT_WINDOW_TITLE)
            .inner_size(980.0, 760.0)
            .min_inner_size(860.0, 620.0)
            .center()
            .build()?;

            show_window(&app_handle, &window)
        })();

        let _ = tx.send(result);
    })?;

    rx.recv()
        .map_err(|_| tauri::Error::FailedToReceiveMessage)?
}

pub fn close_service_management_window<R: Runtime>(app: &AppHandle<R>) {
    let app_handle = app.clone();
    let _ = app.run_on_main_thread(move || {
        if let Some(window) = app_handle.get_webview_window(SERVICE_MANAGEMENT_WINDOW_LABEL) {
            if let Err(err) = window.close() {
                eprintln!("failed to close service management window: {err}");
            }
        }
    });
}

pub fn show_main_window(app: &AppHandle) -> tauri::Result<()> {
    let Some(window) = app.get_webview_window(WINDOW_LABEL) else {
        return Ok(());
    };

    show_window(app, &window)
}

fn hide_main_window(app: &AppHandle) -> tauri::Result<()> {
    let Some(window) = app.get_webview_window(WINDOW_LABEL) else {
        return Ok(());
    };

    hide_window(app, &window)
}

fn toggle_main_window(app: &AppHandle) -> tauri::Result<()> {
    let Some(window) = app.get_webview_window(WINDOW_LABEL) else {
        return Ok(());
    };

    if window.is_visible()? {
        hide_window(app, &window)
    } else {
        show_window(app, &window)
    }
}

pub fn open_data_dir<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    let data_dir = app.path().app_data_dir()?.join("data");
    open_directory(app, data_dir)
}

pub fn open_log_dir<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    let log_dir = app.path().app_log_dir()?;
    open_directory(app, log_dir)
}

pub fn open_app_data_dir<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    let data_dir = app.path().app_data_dir()?;
    open_directory(app, data_dir)
}

fn open_directory<R: Runtime>(app: &AppHandle<R>, path: PathBuf) -> tauri::Result<()> {
    if let Err(err) = fs::create_dir_all(&path) {
        eprintln!("failed to create directory {}: {err}", path.display());
    }

    #[allow(deprecated)]
    app.shell()
        .open(path.to_string_lossy().to_string(), None)
        .map_err(|err| std::io::Error::other(err.to_string()).into())
}

fn build_runtime_script(data_dir: &Path) -> String {
    let payload = serde_json::json!({
      "isDesktopApp": true,
      "platform": "tauri",
      "dataDir": data_dir.to_string_lossy(),
      "serviceManagementUrl": SERVICE_MANAGEMENT_URL,
      "serviceManagementProtocol": SERVICE_MANAGEMENT_PROTOCOL,
    });

    format!(
        "window.__NEW_API_DESKTOP_RUNTIME__ = Object.freeze({});",
        payload
    )
}

fn show_window<R: Runtime>(app: &AppHandle<R>, window: &WebviewWindow<R>) -> tauri::Result<()> {
    if window.is_minimized()? {
        window.unminimize()?;
    }

    window.show()?;
    window.set_focus()?;
    set_dock_visibility(app, true);
    Ok(())
}

fn hide_window<R: Runtime>(app: &AppHandle<R>, window: &WebviewWindow<R>) -> tauri::Result<()> {
    window.hide()?;
    set_dock_visibility(app, false);
    Ok(())
}

#[cfg(target_os = "macos")]
fn set_dock_visibility<R: Runtime>(app: &AppHandle<R>, visible: bool) {
    if let Err(err) = app.set_dock_visibility(visible) {
        eprintln!("failed to update dock visibility: {err}");
    }
}

#[cfg(not(target_os = "macos"))]
fn set_dock_visibility<R: Runtime>(_app: &AppHandle<R>, _visible: bool) {}
