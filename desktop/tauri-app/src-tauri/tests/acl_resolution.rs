//! 打包后主窗口的 origin 是 http://127.0.0.1:<port>，Tauri 判定为 remote。
//! 没有 app ACL manifest 时，capability 无法引用 allow-open-external-url，
//! invoke 会被拒绝，桌面端 OAuth 的"用系统浏览器打开"整条断掉。
//!
//! 这里直接读构建期生成的 ACL 产物做解析，不依赖打包运行：
//! gen/schemas 由 build.rs 在编译前生成，include_str! 在测试编译时必然已存在。

use std::collections::BTreeMap;

use tauri::utils::acl::{
    capability::Capability, manifest::Manifest, resolved::Resolved, ExecutionContext,
};
use tauri::utils::platform::Target;

fn acl_manifests() -> BTreeMap<String, Manifest> {
    serde_json::from_str(include_str!("../gen/schemas/acl-manifests.json"))
        .expect("generated acl manifests must parse")
}

fn capabilities() -> BTreeMap<String, Capability> {
    serde_json::from_str(include_str!("../gen/schemas/capabilities.json"))
        .expect("generated capabilities must parse")
}

#[test]
fn open_external_url_is_allowed_from_the_packaged_window_origin() {
    let resolved = Resolved::resolve(&acl_manifests(), capabilities(), Target::MacOS)
        .expect("capabilities must resolve");
    assert!(
        resolved.has_app_acl,
        "build.rs must declare an app manifest so capabilities can reference app permissions"
    );
    let list = resolved
        .allowed_commands
        .get("open_external_url")
        .expect("open_external_url must resolve to an allowed command");
    let remote = list
        .iter()
        .find_map(|cmd| match &cmd.context {
            ExecutionContext::Remote { url } => Some(url),
            _ => None,
        })
        .expect("a Remote context is required for the packaged main window");
    for (raw, want) in [
        ("http://127.0.0.1:3000/api/status", true),
        ("http://127.0.0.1:4567/", true),
        ("http://localhost:3000/", false),
        ("http://evil.example.com/", false),
        ("https://127.0.0.1:3000/", false),
    ] {
        let url = tauri::Url::parse(raw).expect("test url must parse");
        assert_eq!(remote.test(&url), want, "remote pattern mismatch for {raw}");
    }
}

#[test]
fn removing_the_remote_capability_removes_remote_access() {
    let mut caps = capabilities();
    caps.remove("main-window-remote");
    let resolved = Resolved::resolve(&acl_manifests(), caps, Target::MacOS)
        .expect("capabilities must resolve");
    let has_remote = resolved
        .allowed_commands
        .get("open_external_url")
        .map(|list| {
            list.iter()
                .any(|cmd| matches!(cmd.context, ExecutionContext::Remote { .. }))
        })
        .unwrap_or(false);
    assert!(
        !has_remote,
        "reverse check: the Remote context must come from the remote capability alone"
    );
}

#[test]
fn without_the_app_manifest_the_capability_cannot_reference_the_command() {
    let mut acl = acl_manifests();
    acl.remove("__app-acl__");
    let err = Resolved::resolve(&acl, capabilities(), Target::MacOS)
        .expect_err("the app permission must not resolve without the app manifest");
    let message = err.to_string();
    assert!(
        message.contains("app manifest"),
        "the failure must point at the missing app manifest, got: {message}"
    );
}
