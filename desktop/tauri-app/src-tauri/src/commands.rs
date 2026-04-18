use tauri::{AppHandle, Runtime};
use tauri_plugin_shell::ShellExt;

#[tauri::command]
pub fn open_external_url<R: Runtime>(app: AppHandle<R>, url: String) -> Result<(), String> {
    let target = url.trim();
    if target.is_empty() {
        return Err("missing external url".to_string());
    }
    if !target.starts_with("http://") && !target.starts_with("https://") {
        return Err("external url must start with http:// or https://".to_string());
    }

    #[allow(deprecated)]
    app.shell()
        .open(target.to_string(), None)
        .map_err(|err| err.to_string())
}
