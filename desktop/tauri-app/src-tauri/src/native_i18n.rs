use std::env;

#[derive(Copy, Clone, Debug, Eq, PartialEq)]
enum NativeLanguage {
    Zh,
    En,
}

pub struct TrayMenuText {
    pub show_main_window: &'static str,
    pub service_management: &'static str,
    pub open_data_dir: &'static str,
    pub open_log_dir: &'static str,
    pub quit: &'static str,
}

pub struct AppMenuText {
    pub app_menu: String,
    pub about: String,
    pub services: &'static str,
    pub hide_app: String,
    pub hide_others: &'static str,
    pub show_all: &'static str,
    pub quit_app: String,
    pub file_menu: &'static str,
    pub close_window: &'static str,
    pub edit_menu: &'static str,
    pub undo: &'static str,
    pub redo: &'static str,
    pub cut: &'static str,
    pub copy: &'static str,
    pub paste: &'static str,
    pub select_all: &'static str,
    pub view_menu: &'static str,
    pub fullscreen: &'static str,
    pub window_menu: &'static str,
    pub minimize: &'static str,
    pub zoom: &'static str,
    pub help_menu: &'static str,
}

pub fn tray_menu_text() -> TrayMenuText {
    match resolve_language() {
        NativeLanguage::Zh => TrayMenuText {
            show_main_window: "显示 New API",
            service_management: "服务管理",
            open_data_dir: "打开数据目录",
            open_log_dir: "打开日志目录",
            quit: "退出",
        },
        NativeLanguage::En => TrayMenuText {
            show_main_window: "Show New API",
            service_management: "Service Management",
            open_data_dir: "Open Data Directory",
            open_log_dir: "Open Log Directory",
            quit: "Quit",
        },
    }
}

pub fn service_management_window_title() -> &'static str {
    match resolve_language() {
        NativeLanguage::Zh => "New API 服务管理",
        NativeLanguage::En => "New API Service Management",
    }
}

pub fn app_menu_text(app_name: &str) -> AppMenuText {
    match resolve_language() {
        NativeLanguage::Zh => AppMenuText {
            app_menu: app_name.to_string(),
            about: format!("关于 {app_name}"),
            services: "服务",
            hide_app: format!("隐藏 {app_name}"),
            hide_others: "隐藏其他",
            show_all: "全部显示",
            quit_app: format!("退出 {app_name}"),
            file_menu: "文件",
            close_window: "关闭窗口",
            edit_menu: "编辑",
            undo: "撤销",
            redo: "重做",
            cut: "剪切",
            copy: "复制",
            paste: "粘贴",
            select_all: "全选",
            view_menu: "视图",
            fullscreen: "进入全屏",
            window_menu: "窗口",
            minimize: "最小化",
            zoom: "缩放",
            help_menu: "帮助",
        },
        NativeLanguage::En => AppMenuText {
            app_menu: app_name.to_string(),
            about: format!("About {app_name}"),
            services: "Services",
            hide_app: format!("Hide {app_name}"),
            hide_others: "Hide Others",
            show_all: "Show All",
            quit_app: format!("Quit {app_name}"),
            file_menu: "File",
            close_window: "Close Window",
            edit_menu: "Edit",
            undo: "Undo",
            redo: "Redo",
            cut: "Cut",
            copy: "Copy",
            paste: "Paste",
            select_all: "Select All",
            view_menu: "View",
            fullscreen: "Enter Full Screen",
            window_menu: "Window",
            minimize: "Minimize",
            zoom: "Zoom",
            help_menu: "Help",
        },
    }
}

fn resolve_language() -> NativeLanguage {
    let locale = detect_locale();
    if is_chinese_locale(locale.as_deref()) {
        NativeLanguage::Zh
    } else {
        NativeLanguage::En
    }
}

fn detect_locale() -> Option<String> {
    sys_locale::get_locale().or_else(|| {
        ["LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"]
            .iter()
            .find_map(|key| env::var(key).ok())
    })
}

fn is_chinese_locale(locale: Option<&str>) -> bool {
    let normalized = locale
        .unwrap_or_default()
        .trim()
        .to_ascii_lowercase()
        .replace('_', "-");
    normalized.starts_with("zh")
}
