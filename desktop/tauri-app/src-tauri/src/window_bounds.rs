#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Rect {
    pub x: i32,
    pub y: i32,
    pub width: u32,
    pub height: u32,
}

impl Rect {
    fn right(self) -> i64 {
        self.x as i64 + self.width as i64
    }

    fn bottom(self) -> i64 {
        self.y as i64 + self.height as i64
    }
}

pub fn is_visible_in_any_area(window: Rect, areas: &[Rect]) -> bool {
    if window.width == 0 || window.height == 0 {
        return false;
    }

    areas.iter().copied().any(|area| intersects(window, area))
}

pub fn center_position_in_area(window_width: u32, window_height: u32, area: Rect) -> (i32, i32) {
    let available_width = i64::from(area.width.saturating_sub(window_width));
    let available_height = i64::from(area.height.saturating_sub(window_height));
    let centered_x = i64::from(area.x) + available_width / 2;
    let centered_y = i64::from(area.y) + available_height / 2;
    (
        centered_x.clamp(i32::MIN as i64, i32::MAX as i64) as i32,
        centered_y.clamp(i32::MIN as i64, i32::MAX as i64) as i32,
    )
}

fn intersects(window: Rect, area: Rect) -> bool {
    let left = i64::from(window.x).max(i64::from(area.x));
    let top = i64::from(window.y).max(i64::from(area.y));
    let right = window.right().min(area.right());
    let bottom = window.bottom().min(area.bottom());
    right > left && bottom > top
}
