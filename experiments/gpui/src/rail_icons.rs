//! Embedded rail icons from the same family as the Web workspace.
//! Fixed asset keys only: note content cannot supply paths or SVG bytes.
use gpui::{AssetSource, Div, Stateful, div, prelude::*, px, rgb, svg};
use std::borrow::Cow;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Icon {
    Back,
    Forward,
    Recent,
    Home,
    Search,
    Calendar,
    Graph,
    Find,
    Reload,
    Theme,
    Info,
    Hierarchy,
    New,
}
impl Icon {
    pub(crate) const ALL: [Self; 13] = [
        Self::Back,
        Self::Forward,
        Self::Recent,
        Self::Home,
        Self::Search,
        Self::Calendar,
        Self::Graph,
        Self::Find,
        Self::Reload,
        Self::Theme,
        Self::Info,
        Self::Hierarchy,
        Self::New,
    ];
    pub(crate) fn path(self) -> &'static str {
        match self {
            Self::Back => "rail/ArrowLeft.svg",
            Self::Forward => "rail/ArrowRight.svg",
            Self::Recent => "rail/History.svg",
            Self::Home => "rail/Brand.svg",
            Self::Search => "rail/Search.svg",
            Self::Calendar => "rail/Calendar.svg",
            Self::Graph => "rail/Affiliate.svg",
            Self::Find => "rail/FileSearch.svg",
            Self::Reload => "rail/Rotate2.svg",
            Self::Theme => "rail/SunMoon.svg",
            Self::Info => "rail/InfoCircle.svg",
            Self::Hierarchy => "rail/Sitemap.svg",
            Self::New => "rail/Plus.svg",
        }
    }
    pub(crate) fn label(self) -> &'static str {
        match self {
            Self::Back => "Back",
            Self::Forward => "Forward",
            Self::Recent => "Recent visits",
            Self::Home => "Home",
            Self::Search => "Search notes",
            Self::Calendar => "Calendar",
            Self::Graph => "Full graph",
            Self::Find => "Find in note",
            Self::Reload => "Reload",
            Self::Theme => "Toggle theme",
            Self::Info => "Diagnostics",
            Self::Hierarchy => "Hierarchy",
            Self::New => "New note",
        }
    }
}

pub(crate) struct Assets;
impl AssetSource for Assets {
    fn load(&self, path: &str) -> gpui::Result<Option<Cow<'static, [u8]>>> {
        let bytes: &'static [u8] = match path {
            "rail/ArrowLeft.svg" => include_bytes!("../assets/icons/ArrowLeft.svg"),
            "rail/ArrowRight.svg" => include_bytes!("../assets/icons/ArrowRight.svg"),
            "rail/History.svg" => include_bytes!("../assets/icons/History.svg"),
            "rail/Brand.svg" => include_bytes!("../assets/icons/Brand.svg"),
            "rail/Search.svg" => include_bytes!("../assets/icons/Search.svg"),
            "rail/Calendar.svg" => include_bytes!("../assets/icons/Calendar.svg"),
            "rail/Affiliate.svg" => include_bytes!("../assets/icons/Affiliate.svg"),
            "rail/FileSearch.svg" => include_bytes!("../assets/icons/FileSearch.svg"),
            "rail/Rotate2.svg" => include_bytes!("../assets/icons/Rotate2.svg"),
            "rail/SunMoon.svg" => include_bytes!("../assets/icons/SunMoon.svg"),
            "rail/InfoCircle.svg" => include_bytes!("../assets/icons/InfoCircle.svg"),
            "rail/Sitemap.svg" => include_bytes!("../assets/icons/Sitemap.svg"),
            "rail/Plus.svg" => include_bytes!("../assets/icons/Plus.svg"),
            _ => return Ok(None),
        };
        Ok(Some(Cow::Borrowed(bytes)))
    }
    fn list(&self, path: &str) -> gpui::Result<Vec<gpui::SharedString>> {
        Ok(if path == "rail" || path == "rail/" {
            Icon::ALL
                .into_iter()
                .map(|icon| icon.path().into())
                .collect()
        } else {
            Vec::new()
        })
    }
}

struct Hint {
    label: &'static str,
    color: u32,
    background: u32,
}
impl gpui::Render for Hint {
    fn render(&mut self, _: &mut gpui::Window, _: &mut gpui::Context<Self>) -> impl IntoElement {
        div()
            .px_2()
            .py_1()
            .rounded_md()
            .bg(rgb(self.background))
            .text_color(rgb(self.color))
            .text_size(px(12.))
            .child(self.label)
    }
}

pub(crate) fn button(id: &'static str, icon: Icon, color: u32, background: u32) -> Stateful<Div> {
    div()
        .id(id)
        .w(px(40.))
        .h(px(40.))
        .flex_shrink_0()
        .flex()
        .items_center()
        .justify_center()
        .rounded_md()
        .child(
            svg()
                .path(icon.path())
                .w(px(20.))
                .h(px(20.))
                .text_color(rgb(color)),
        )
        .tooltip(move |_, cx| {
            cx.new(|_| Hint {
                label: icon.label(),
                color,
                background,
            })
            .into()
        })
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn all_buttons_have_distinct_embedded_assets_and_labels() {
        let mut seen = std::collections::HashSet::new();
        for icon in Icon::ALL {
            assert!(seen.insert(icon.path()));
            assert!(!icon.label().is_empty());
            let bytes = Assets.load(icon.path()).unwrap().unwrap();
            let svg = std::str::from_utf8(&bytes).unwrap();
            assert!(svg.contains("<svg ") && svg.contains("<path "));
            assert!(!svg.contains("<text") && !svg.contains("href=") && !svg.contains("<script"));
            if icon != Icon::Home {
                for attribute in [
                    "viewBox=\"0 0 24 24\"",
                    "stroke-width=\"1.5\"",
                    "stroke-linecap=\"round\"",
                    "stroke-linejoin=\"round\"",
                ] {
                    assert!(svg.contains(attribute), "{icon:?}: {attribute}");
                }
            }
        }
        assert_eq!(Assets.list("rail").unwrap().len(), Icon::ALL.len());
    }
    #[test]
    fn asset_source_never_opens_unrecognized_paths() {
        for path in [
            "",
            "/etc/passwd",
            "rail/../Brand.svg",
            "https://example.com/icon.svg",
            "rail/unknown.svg",
        ] {
            assert!(Assets.load(path).unwrap().is_none());
            assert!(Assets.list(path).unwrap().is_empty());
        }
    }
}
