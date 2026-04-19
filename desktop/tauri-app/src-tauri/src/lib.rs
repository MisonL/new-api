mod commands;
mod constants;
mod native_i18n;
mod runtime;
pub mod runtime_support;
mod service_management;
mod state;
pub mod window_bounds;
mod windowing;

use state::{mark_quitting, DesktopState};
use tauri::{Manager, RunEvent};

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let builder = tauri::Builder::default()
        .register_uri_scheme_protocol(constants::SERVICE_MANAGEMENT_PROTOCOL, |ctx, request| {
            service_management::handle_request(ctx, request)
        })
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            let target = runtime_support::resolve_single_instance_focus_target(
                app.get_webview_window(constants::WINDOW_LABEL).is_some(),
                app.get_webview_window(constants::SERVICE_MANAGEMENT_WINDOW_LABEL)
                    .is_some(),
            );

            match target {
                runtime_support::SingleInstanceFocusTarget::MainWindow => {
                    let _ = windowing::show_main_window(app);
                }
                runtime_support::SingleInstanceFocusTarget::ServiceManagementWindow => {
                    let _ = windowing::open_service_management_window(app);
                }
                runtime_support::SingleInstanceFocusTarget::None => {}
            }
        }))
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .invoke_handler(tauri::generate_handler![commands::open_external_url])
        .on_window_event(windowing::handle_window_event)
        .setup(|app| {
            let log_level = if cfg!(debug_assertions) {
                log::LevelFilter::Debug
            } else {
                log::LevelFilter::Info
            };
            app.handle().plugin(
                tauri_plugin_log::Builder::default()
                    .level(log_level)
                    .build(),
            )?;

            app.manage(DesktopState::default());
            windowing::create_native_app_menu(app.handle())?;
            windowing::create_tray(app.handle())?;
            runtime::spawn_bootstrap_desktop_runtime(app.handle().clone());

            Ok(())
        });

    let app = builder
        .build(tauri::generate_context!())
        .expect("error while building tauri application");

    app.run(|app_handle, event| match event {
        RunEvent::Exit | RunEvent::ExitRequested { .. } => {
            mark_quitting(app_handle);
            runtime::stop_sidecar(app_handle);
        }
        _ => {}
    });
}
