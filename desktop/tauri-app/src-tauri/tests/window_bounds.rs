use new_api_tauri_desktop_lib::window_bounds::{
    center_position_in_area, is_visible_in_any_area, Rect,
};

#[test]
fn window_inside_work_area_is_visible() {
    let window = Rect {
        x: 120,
        y: 80,
        width: 1080,
        height: 720,
    };
    let area = Rect {
        x: 0,
        y: 0,
        width: 1512,
        height: 945,
    };

    assert!(is_visible_in_any_area(window, &[area]));
}

#[test]
fn window_on_removed_monitor_is_not_visible() {
    let window = Rect {
        x: -1436,
        y: 91,
        width: 1080,
        height: 720,
    };
    let area = Rect {
        x: 0,
        y: 0,
        width: 1512,
        height: 945,
    };

    assert!(!is_visible_in_any_area(window, &[area]));
}

#[test]
fn center_position_in_area_returns_centered_coordinates() {
    let area = Rect {
        x: 0,
        y: 0,
        width: 1512,
        height: 945,
    };

    assert_eq!(center_position_in_area(1080, 720, area), (216, 112));
}

#[test]
fn oversized_window_centers_to_area_origin() {
    let area = Rect {
        x: 100,
        y: 60,
        width: 800,
        height: 600,
    };

    assert_eq!(center_position_in_area(1200, 900, area), (100, 60));
}
