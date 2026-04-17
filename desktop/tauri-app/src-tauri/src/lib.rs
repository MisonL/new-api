mod constants;
mod runtime;
pub mod runtime_support;
mod service_management;
mod state;
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
            if app.get_webview_window(constants::WINDOW_LABEL).is_some() {
                let _ = windowing::show_main_window(app);
            } else if app
                .get_webview_window(constants::SERVICE_MANAGEMENT_WINDOW_LABEL)
                .is_some()
            {
                let _ = windowing::open_service_management_window(app);
            }
        }))
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .on_window_event(windowing::handle_window_event)
        .setup(|app| {
            if cfg!(debug_assertions) {
                app.handle().plugin(
                    tauri_plugin_log::Builder::default()
                        .level(log::LevelFilter::Info)
                        .build(),
                )?;
            }

            app.manage(DesktopState::default());
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
