fn main() {
    // 声明应用级 ACL manifest：自定义命令必须在 manifest 里登记，能力文件
    // 才能引用对应的 allow-<command> 权限。没有这份 manifest 时，打包后
    // 主窗口的 origin 是 http://127.0.0.1:<port>（非 dev），Tauri 判定为
    // remote，而 capability 没有给它配 remote URL，于是 invoke 被 ACL 拒绝。
    tauri_build::try_build(
        tauri_build::Attributes::new()
            .app_manifest(tauri_build::AppManifest::new().commands(&["open_external_url"])),
    )
    .expect("failed to run tauri-build");
}
