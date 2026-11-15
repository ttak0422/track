//! Inert, bounded projection of engine-produced query snapshots.
use serde_json::{Map, Value};
use std::collections::BTreeSet;

pub const LIST_PAGE: usize = 100;
pub const LANE_PAGE: usize = 4;
pub const CARD_PAGE: usize = 50;
pub const GALLERY_PAGE: usize = 4;
const MAX_BYTES: usize = 256 * 1024;
const MAX_COLUMNS: usize = 32;
const MAX_GROUPS: usize = 128;
const MAX_ROWS: usize = 4096;
const MAX_CELL_BYTES: usize = 16 * 1024;
const MAX_LABEL_BYTES: usize = 4 * 1024;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Layout {
    List,
    Board,
    Calendar,
    Gallery,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct View {
    pub layout: Layout,
    pub show_title: bool,
    pub key: Option<String>,
    pub columns: Vec<String>,
    pub groups: Vec<Group>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Group {
    pub name: Option<String>,
    pub rows: Vec<Row>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Row {
    pub title: String,
    pub target: Option<String>,
    pub cells: Vec<String>,
    pub cover: Option<String>,
    pub icon: Option<String>,
}

/// Gregorian civil month; calendar projection never consults a clock or timezone.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub struct CalendarMonth {
    pub year: u16,
    pub month: u8,
}

impl CalendarMonth {
    pub fn label(self) -> String {
        format!("{:04} / {:02}", self.year, self.month)
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CalendarDay<'a> {
    pub day: u8,
    pub group_index: Option<usize>,
    pub titles: Vec<&'a str>,
    pub excess: usize,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CalendarPage<'a> {
    pub index: usize,
    pub count: usize,
    pub month: CalendarMonth,
    /// Sunday-first rectangular weeks. None slots are inert padding.
    pub cells: Vec<Option<CalendarDay<'a>>>,
}

fn days_in_month(year: u16, month: u8) -> u8 {
    match month {
        4 | 6 | 9 | 11 => 30,
        2 if year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) => 29,
        2 => 28,
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        _ => 0,
    }
}

fn parse_date(text: &str) -> Option<(CalendarMonth, u8)> {
    let bytes = text.as_bytes();
    if bytes.len() != 10
        || bytes[4] != b'-'
        || bytes[7] != b'-'
        || !bytes
            .iter()
            .enumerate()
            .all(|(i, b)| i == 4 || i == 7 || b.is_ascii_digit())
    {
        return None;
    }
    let year = text[..4].parse::<u16>().ok()?;
    let month = text[5..7].parse::<u8>().ok()?;
    let day = text[8..].parse::<u8>().ok()?;
    if year == 0 || day == 0 || day > days_in_month(year, month) {
        return None;
    }
    Some((CalendarMonth { year, month }, day))
}

/// Sunday = 0. Day 0001-01-01 is Monday in the proleptic Gregorian calendar.
fn first_weekday(month: CalendarMonth) -> usize {
    let previous_year = u32::from(month.year) - 1;
    let previous_month_days: u32 = (1..month.month)
        .map(|m| u32::from(days_in_month(month.year, m)))
        .sum();
    ((365 * previous_year + previous_year / 4 - previous_year / 100
        + previous_year / 400
        + previous_month_days
        + 1)
        % 7) as usize
}

impl View {
    /// Flatten engine order while retaining structural row identities, never titles.
    pub fn gallery_page(&self, requested: usize) -> Vec<(usize, usize, &Row)> {
        if self.layout != Layout::Gallery {
            return Vec::new();
        }
        let last_page = self.total_rows().saturating_sub(1) / GALLERY_PAGE;
        self.groups
            .iter()
            .enumerate()
            .flat_map(|(group_index, group)| {
                group
                    .rows
                    .iter()
                    .enumerate()
                    .map(move |(row_index, row)| (group_index, row_index, row))
            })
            .skip(requested.min(last_page) * GALLERY_PAGE)
            .take(GALLERY_PAGE)
            .collect()
    }

    /// Only supplied months exist. Even an empty supplied date group establishes its month.
    pub fn calendar_months(&self) -> Vec<CalendarMonth> {
        if self.layout != Layout::Calendar {
            return Vec::new();
        }
        self.groups
            .iter()
            .filter_map(|group| parse_date(group.name.as_deref()?).map(|(month, _)| month))
            .collect::<BTreeSet<_>>()
            .into_iter()
            .collect()
    }

    /// Materializes a single bounded month, not all months on every page.
    pub fn calendar_page(&self, requested: usize) -> Option<CalendarPage<'_>> {
        let months = self.calendar_months();
        let count = months.len();
        let index = requested.min(count.checked_sub(1)?);
        let month = months[index];
        let mut cells = vec![None; first_weekday(month)];
        for day in 1..=days_in_month(month.year, month.month) {
            let group = self.groups.iter().enumerate().find(|(_, group)| {
                group.name.as_deref().and_then(parse_date) == Some((month, day))
            });
            let (group_index, titles, excess) = match group {
                Some((index, group)) => (
                    Some(index),
                    group
                        .rows
                        .iter()
                        .take(3)
                        .map(|row| row.title.as_str())
                        .collect(),
                    group.rows.len().saturating_sub(3),
                ),
                None => (None, Vec::new(), 0),
            };
            cells.push(Some(CalendarDay {
                day,
                group_index,
                titles,
                excess,
            }));
        }
        while cells.len() % 7 != 0 {
            cells.push(None);
        }
        Some(CalendarPage {
            index,
            count,
            month,
            cells,
        })
    }
    pub fn total_rows(&self) -> usize {
        self.groups.iter().map(|group| group.rows.len()).sum()
    }

    pub fn metadata(&self, row: &Row) -> Vec<String> {
        self.columns
            .iter()
            .zip(&row.cells)
            .filter(|(column, value)| {
                column.as_str() != "title"
                    && !value.is_empty()
                    && !(self.layout == Layout::Board
                        && self.key.as_deref() == Some(column.as_str()))
            })
            .map(|(column, value)| format!("{column}: {value}"))
            .collect()
    }
}

fn object<'a>(value: &'a Value, name: &str) -> Result<&'a Map<String, Value>, String> {
    value
        .as_object()
        .ok_or_else(|| format!("{name} must be an object"))
}

fn array<'a>(object: &'a Map<String, Value>, name: &str) -> Result<&'a Vec<Value>, String> {
    object
        .get(name)
        .and_then(Value::as_array)
        .ok_or_else(|| format!("{name} must be an array"))
}

fn string<'a>(value: &'a Value, name: &str, max: usize) -> Result<&'a str, String> {
    let value = value
        .as_str()
        .ok_or_else(|| format!("{name} must be a string"))?;
    if value.len() > max {
        return Err(format!("{name} exceeds text limit"));
    }
    Ok(value)
}

fn optional_string<'a>(
    object: &'a Map<String, Value>,
    name: &str,
    max: usize,
) -> Result<Option<&'a str>, String> {
    object
        .get(name)
        .map(|value| string(value, name, max))
        .transpose()
}

/// Validate the entire bounded document before cloning any typed UI data.
/// Targets remain opaque resolver input; cover/icon strings remain display data.
pub fn decode(source: &str) -> Result<View, String> {
    if source.len() > MAX_BYTES {
        return Err("Query snapshot exceeds 256 KiB limit".into());
    }
    let value: Value =
        serde_json::from_str(source).map_err(|_| "Malformed query snapshot JSON".to_string())?;
    let root = object(&value, "Query snapshot")?;
    let layout = match root.get("layout").and_then(Value::as_str) {
        Some("list") => Layout::List,
        Some("board") => Layout::Board,
        Some("calendar") => Layout::Calendar,
        Some("gallery") => Layout::Gallery,
        Some(_) => {
            return Err(
                "Unsupported query layout (only list, board, calendar and gallery are supported)"
                    .into(),
            );
        }
        None => return Err("layout must be a string".into()),
    };
    let show_title = match root.get("showTitle") {
        None => true,
        Some(Value::Bool(value)) => *value,
        Some(_) => return Err("showTitle must be a boolean".into()),
    };
    let key = optional_string(root, "key", MAX_LABEL_BYTES)?;
    let columns = array(root, "columns")?;
    if columns.len() > MAX_COLUMNS {
        return Err("Query snapshot exceeds 32 columns".into());
    }
    for column in columns {
        string(column, "column", MAX_LABEL_BYTES)?;
    }
    let groups = array(root, "groups")?;
    if groups.len() > MAX_GROUPS {
        return Err("Query snapshot exceeds 128 groups".into());
    }
    let mut total_rows = 0;
    let mut calendar_dates = BTreeSet::new();
    for group in groups {
        let group = object(group, "group")?;
        let name = optional_string(group, "name", MAX_LABEL_BYTES)?;
        if layout == Layout::Calendar {
            let date = name.and_then(parse_date).ok_or(
                "Invalid calendar date: expected a real Gregorian YYYY-MM-DD in years 0001..9999",
            )?;
            if !calendar_dates.insert(date) {
                return Err("Duplicate calendar date groups are ambiguous".into());
            }
        }
        let rows = array(group, "rows")?;
        total_rows += rows.len();
        if total_rows > MAX_ROWS {
            return Err("Query snapshot exceeds 4096 rows".into());
        }
        for row in rows {
            let row = object(row, "row")?;
            string(
                row.get("title").ok_or("title is required")?,
                "title",
                MAX_LABEL_BYTES,
            )?;
            for field in ["target", "cover", "icon"] {
                optional_string(row, field, MAX_LABEL_BYTES)?;
            }
            let cells = array(row, "cells")?;
            if cells.len() != columns.len() {
                return Err("Row cells must match the column count".into());
            }
            for cell in cells {
                string(cell, "cell", MAX_CELL_BYTES)?;
            }
        }
    }
    // All access below is covered by the validation above; no partial result escapes.
    Ok(View {
        layout,
        show_title,
        key: key.map(str::to_owned),
        columns: columns
            .iter()
            .map(|v| v.as_str().unwrap().to_owned())
            .collect(),
        groups: groups
            .iter()
            .map(|group| Group {
                name: group.get("name").map(|v| v.as_str().unwrap().to_owned()),
                rows: group["rows"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .map(|row| Row {
                        title: row["title"].as_str().unwrap().to_owned(),
                        target: row.get("target").map(|v| v.as_str().unwrap().to_owned()),
                        cover: row.get("cover").map(|v| v.as_str().unwrap().to_owned()),
                        icon: row.get("icon").map(|v| v.as_str().unwrap().to_owned()),
                        cells: row["cells"]
                            .as_array()
                            .unwrap()
                            .iter()
                            .map(|v| v.as_str().unwrap().to_owned())
                            .collect(),
                    })
                    .collect(),
            })
            .collect(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn base() -> Value {
        json!({"layout":"list","columns":["title","status","Title","empty"],"groups":[{"rows":[{"title":"same","cells":["same","doing","case-sensitive",""]}]}]})
    }
    fn read(value: &Value) -> Result<View, String> {
        decode(&value.to_string())
    }
    #[test]
    fn empty_layouts_defaults_and_unknown_fields() {
        for layout in ["list", "board", "calendar", "gallery"] {
            let view = read(&json!({"layout":layout,"columns":[],"groups":[],"future":{"x":null}}))
                .unwrap();
            assert!(view.show_title);
            assert_eq!(view.total_rows(), 0);
        }
        assert!(decode("not json").is_err());
        for layout in ["unknown", "LIST", ""] {
            assert!(
                read(&json!({"layout":layout,"columns":[],"groups":[]}))
                    .unwrap_err()
                    .contains("Unsupported")
            );
        }
    }
    #[test]
    fn metadata_is_exact_and_engine_order_is_preserved() {
        let mut value = base();
        value["key"] = json!("status");
        let list = read(&value).unwrap();
        assert_eq!(
            list.metadata(&list.groups[0].rows[0]),
            ["status: doing", "Title: case-sensitive"]
        );
        value["layout"] = json!("board");
        value["showTitle"] = json!(false);
        value["groups"]
            .as_array_mut()
            .unwrap()
            .push(json!({"name":"","rows":[]}));
        value["groups"]
            .as_array_mut()
            .unwrap()
            .push(json!({"name":"","rows":[{"title":"same","cells":["same","done","last",""]}]}));
        let board = read(&value).unwrap();
        assert!(!board.show_title);
        assert_eq!(board.total_rows(), 2);
        assert_eq!(board.groups[0].name, None);
        assert_eq!(board.groups[1].name.as_deref(), Some(""));
        assert_eq!(
            board.metadata(&board.groups[0].rows[0]),
            ["Title: case-sensitive"]
        );
        assert_eq!(board.metadata(&board.groups[2].rows[0]), ["Title: last"]);
        let hidden = read(&json!({"layout":"board","showTitle":false,"key":"status","columns":["title","status"],"groups":[{"rows":[{"title":"x","cells":["x","doing"]}]}]})).unwrap();
        assert!(hidden.metadata(&hidden.groups[0].rows[0]).is_empty());
        assert_eq!(hidden.total_rows(), 1);
    }
    #[test]
    fn strict_known_types_null_missing_and_alignment() {
        for field in ["layout", "showTitle", "key", "columns", "groups"] {
            for invalid in [Value::Null, json!(42), json!({})] {
                let mut value = base();
                value[field] = invalid;
                assert!(read(&value).is_err(), "{field}");
            }
        }
        for field in ["layout", "columns", "groups"] {
            let mut value = base();
            value.as_object_mut().unwrap().remove(field);
            assert!(read(&value).is_err());
        }
        for field in ["name", "rows"] {
            let mut value = base();
            value["groups"][0][field] = Value::Null;
            assert!(read(&value).is_err());
        }
        for field in ["title", "cells", "target", "cover", "icon"] {
            for invalid in [Value::Null, json!(false), json!(9)] {
                let mut value = base();
                value["groups"][0]["rows"][0][field] = invalid;
                assert!(read(&value).is_err(), "{field}");
            }
        }
        for cells in [
            json!([]),
            json!(["a", "b", "c"]),
            json!(["a", "b", "c", "d", "e"]),
            json!(["a", "b", "c", null]),
        ] {
            let mut value = base();
            value["groups"][0]["rows"][0]["cells"] = cells;
            assert!(read(&value).is_err());
        }
        for path in [0, 1, 2] {
            let mut value = base();
            match path {
                0 => value["columns"][0] = json!(false),
                1 => value["groups"][0] = json!([]),
                _ => value["groups"][0]["rows"][0] = json!("row"),
            }
            assert!(read(&value).is_err());
        }
    }
    #[test]
    fn hostile_strings_and_duplicates_remain_literal() {
        let text =
            "日本語 <script>alert(1)</script> [[@42]] javascript:alert(1) https://evil.invalid/";
        let value = json!({"layout":"board","columns":["owner","owner"],"groups":[{"name":"(no tags)","rows":[{"title":text,"cells":[text,text],"target":"@42","cover":"https://evil.invalid/pixel","icon":"<svg onload=alert(1)>"}]}]});
        let view = read(&value).unwrap();
        assert_eq!(view.groups[0].name.as_deref(), Some("(no tags)"));
        assert_eq!(view.groups[0].rows[0].title, text);
        assert_eq!(
            view.metadata(&view.groups[0].rows[0]),
            [format!("owner: {text}"), format!("owner: {text}")]
        );
    }
    #[test]
    fn byte_and_array_caps_include_exact_boundaries() {
        for count in [0, 1, 100, 101, 4096, 4097] {
            let value = json!({"layout":"list","columns":[],"groups":[{"rows":vec![json!({"title":"","cells":[]});count]}]});
            assert_eq!(read(&value).is_ok(), count <= MAX_ROWS);
            if count <= MAX_ROWS {
                assert_eq!(read(&value).unwrap().total_rows(), count);
            }
        }
        for count in [MAX_COLUMNS, MAX_COLUMNS + 1] {
            assert_eq!(
                read(&json!({"layout":"list","columns":vec!["";count],"groups":[]})).is_ok(),
                count == MAX_COLUMNS
            );
        }
        for count in [0, 5, MAX_GROUPS, MAX_GROUPS + 1] {
            assert_eq!(
                read(
                    &json!({"layout":"board","columns":[],"groups":vec![json!({"rows":[]});count]})
                )
                .is_ok(),
                count <= MAX_GROUPS
            );
        }
        for field in ["title", "target", "cover", "icon"] {
            for len in [MAX_LABEL_BYTES, MAX_LABEL_BYTES + 1] {
                let mut value = base();
                value["groups"][0]["rows"][0][field] = json!("a".repeat(len));
                assert_eq!(read(&value).is_ok(), len == MAX_LABEL_BYTES, "{field}");
            }
        }
        for len in [MAX_LABEL_BYTES, MAX_LABEL_BYTES + 1] {
            let mut value = base();
            value["groups"][0]["name"] = json!("a".repeat(len));
            assert_eq!(read(&value).is_ok(), len == MAX_LABEL_BYTES);
        }
        for len in [MAX_CELL_BYTES, MAX_CELL_BYTES + 1] {
            let mut value = base();
            value["groups"][0]["rows"][0]["cells"][0] = json!("a".repeat(len));
            assert_eq!(read(&value).is_ok(), len == MAX_CELL_BYTES);
        }
        let mut unicode = base();
        unicode["groups"][0]["rows"][0]["title"] = json!("界".repeat(MAX_LABEL_BYTES / 3 + 1));
        assert!(read(&unicode).is_err());
        let source = json!({"layout":"list","columns":[],"groups":[]}).to_string();
        let padded = format!("{}{}", source, " ".repeat(MAX_BYTES - source.len()));
        assert!(decode(&padded).is_ok());
        assert!(decode(&(padded + " ")).unwrap_err().contains("256 KiB"));
        assert!(
            decode(&"x".repeat(MAX_BYTES + 1))
                .unwrap_err()
                .contains("256 KiB")
        );
        // The row cap is aggregate, not merely a per-lane cap.
        assert!(read(&json!({"layout":"board","columns":[],"groups":vec![json!({"rows":vec![json!({"title":"","cells":[]});2049]});2]})).is_err());
    }
    #[test]
    fn gallery_optional_faces_and_literal_metadata_preserve_order() {
        let view = read(&json!({"layout":"gallery","showTitle":false,"key":"status",
        "columns":["title","status","Title","empty"],"groups":[
            {"rows":[{"title":"same","cells":["same","doing","literal",""]}]},
            {"rows":[]},
            {"rows":[
                {"title":"same","cells":["same","done","second",""],"cover":"","icon":""},
                {"title":"<b>third</b>","cells":["third","last","",""],
                 "cover":"https://invalid/cover","icon":"<svg>","target":"@opaque"}
            ]}
        ]}))
        .unwrap();
        assert!(!view.show_title);
        let page = view.gallery_page(0);
        assert_eq!(
            page.iter().map(|(g, r, _)| (*g, *r)).collect::<Vec<_>>(),
            [(0, 0), (2, 0), (2, 1)]
        );
        assert_eq!(
            page.iter()
                .map(|(_, _, r)| r.title.as_str())
                .collect::<Vec<_>>(),
            ["same", "same", "<b>third</b>"]
        );
        assert_eq!((&page[0].2.cover, &page[0].2.icon), (&None, &None));
        assert_eq!(page[1].2.cover.as_deref(), Some(""));
        assert_eq!(page[1].2.icon.as_deref(), Some(""));
        assert_eq!(page[2].2.cover.as_deref(), Some("https://invalid/cover"));
        assert_eq!(page[2].2.icon.as_deref(), Some("<svg>"));
        assert_eq!(
            view.metadata(page[0].2),
            ["status: doing", "Title: literal"]
        );
        let hidden = read(
            &json!({"layout":"gallery","showTitle":false,"columns":["title"],
            "groups":[{"rows":[{"title":"still a card","cells":["still a card"]}]}]}),
        )
        .unwrap();
        assert_eq!(hidden.gallery_page(0).len(), 1);
        assert!(hidden.metadata(hidden.gallery_page(0)[0].2).is_empty());
        assert!(read(&base()).unwrap().gallery_page(0).is_empty());
    }

    #[test]
    fn gallery_four_card_pages_reach_every_row_and_clamp() {
        for count in [0usize, 1, 4, 5, 4096] {
            let view = read(&json!({"layout":"gallery","columns":[],
                "groups":[{"rows":(0..count).map(|i|
                    json!({"title":i.to_string(),"cells":[]})).collect::<Vec<_>>()}]}))
            .unwrap();
            let pages = count.div_ceil(GALLERY_PAGE);
            let mut seen = Vec::new();
            for index in 0..pages {
                let page = view.gallery_page(index);
                assert!(!page.is_empty() && page.len() <= GALLERY_PAGE);
                for (group, row, value) in page {
                    assert_eq!(group, 0);
                    assert_eq!(value.title, row.to_string());
                    seen.push(row);
                }
            }
            assert_eq!(seen, (0..count).collect::<Vec<_>>());
            assert_eq!(
                view.gallery_page(usize::MAX),
                view.gallery_page(pages.saturating_sub(1))
            );
        }
    }

    #[test]
    fn gallery_validates_hidden_pages_and_all_existing_caps() {
        let mut value = json!({"layout":"gallery","columns":[],
            "groups":[{"rows":vec![json!({"title":"","cells":[]});5]}]});
        for field in ["cover", "icon", "target", "title", "cells"] {
            let mut malformed = value.clone();
            malformed["groups"][0]["rows"][4][field] = Value::Null;
            assert!(read(&malformed).is_err(), "{field}");
        }
        for field in ["cover", "icon"] {
            for len in [MAX_LABEL_BYTES, MAX_LABEL_BYTES + 1] {
                let mut bounded = value.clone();
                bounded["groups"][0]["rows"][4][field] = json!("x".repeat(len));
                assert_eq!(read(&bounded).is_ok(), len == MAX_LABEL_BYTES);
            }
        }
        value["groups"][0]["rows"] = json!(vec![json!({"title":"","cells":[]}); MAX_ROWS + 1]);
        assert!(read(&value).unwrap_err().contains("4096 rows"));
        value["groups"] = json!(vec![json!({"rows":[]}); MAX_GROUPS + 1]);
        assert!(read(&value).unwrap_err().contains("128 groups"));
        value["groups"] = json!([]);
        value["columns"] = json!(vec![""; MAX_COLUMNS + 1]);
        assert!(read(&value).unwrap_err().contains("32 columns"));
        value["columns"] = json!(["x"]);
        value["groups"] = json!([{"rows":[{"title":"","cells":["x".repeat(MAX_CELL_BYTES + 1)]}]}]);
        assert!(
            read(&value)
                .unwrap_err()
                .contains("cell exceeds text limit")
        );
        let source = json!({"layout":"gallery","columns":[],"groups":[]}).to_string();
        let padded = format!("{}{}", source, " ".repeat(MAX_BYTES - source.len()));
        assert!(decode(&padded).is_ok());
        assert!(decode(&(padded + " ")).unwrap_err().contains("256 KiB"));
    }

    fn calendar(dates: &[&str]) -> View {
        read(
            &json!({"layout":"calendar","columns":[],"groups":dates.iter()
            .map(|date| json!({"name":date,"rows":[]})).collect::<Vec<_>>()}),
        )
        .unwrap()
    }

    #[test]
    fn calendar_exact_real_dates_and_duplicates_are_required() {
        for date in [
            "2026-02-30",
            "1900-02-29",
            "0000-01-01",
            "2026-13-01",
            "2026-00-01",
            "2026-01-00",
            "2026-04-31",
            "2026-7-02",
            "2026-07-2",
            "2026-07-02T00:00",
            " 2026-07-02",
            "２０２６-07-02",
            "10000-01-01",
            "",
        ] {
            let source =
                json!({"layout":"calendar","columns":[],"groups":[{"name":date,"rows":[]}]})
                    .to_string();
            assert!(
                decode(&source)
                    .unwrap_err()
                    .contains("Invalid calendar date"),
                "{date}"
            );
            assert!(source.contains(date)); // The caller retains the unchanged source on error.
        }
        assert!(
            read(&json!({"layout":"calendar","columns":[],"groups":[{"rows":[]}]}))
                .unwrap_err()
                .contains("Invalid calendar date")
        );
        assert!(
            read(&json!({"layout":"calendar","columns":[],"groups":[
            {"name":"2026-07-02","rows":[]},{"name":"2026-07-02","rows":[]}]}))
            .unwrap_err()
            .contains("Duplicate")
        );
        for date in [
            "0001-01-01",
            "0099-12-31",
            "2000-02-29",
            "2024-02-29",
            "9999-12-31",
        ] {
            assert_eq!(calendar(&[date]).calendar_months().len(), 1);
        }
    }

    #[test]
    fn calendar_leap_weekday_and_rectangular_grid_bounds() {
        for (year, length) in [
            (1900, 28),
            (2000, 29),
            (2024, 29),
            (1, 28),
            (99, 28),
            (9999, 28),
        ] {
            assert_eq!(days_in_month(year, 2), length);
        }
        for (date, weekday, length, slots) in [
            ("2026-07-01", 3, 31, 35),
            ("2026-11-01", 0, 30, 35),
            ("2024-02-01", 4, 29, 35),
            ("2026-08-01", 6, 31, 42),
            ("0001-01-01", 1, 31, 35),
            ("9999-12-01", 3, 31, 35),
        ] {
            let view = calendar(&[date]);
            let page = view.calendar_page(0).unwrap();
            assert_eq!(page.cells.len(), slots, "{date}");
            assert!(page.cells[..weekday].iter().all(Option::is_none));
            assert_eq!(
                page.cells
                    .iter()
                    .flatten()
                    .map(|day| day.day)
                    .collect::<Vec<_>>(),
                (1..=length).collect::<Vec<_>>()
            );
            assert!(
                page.cells[weekday + usize::from(length)..]
                    .iter()
                    .all(Option::is_none)
            );
        }
        assert_eq!(
            calendar(&["0099-01-01"])
                .calendar_page(0)
                .unwrap()
                .month
                .label(),
            "0099 / 01"
        );
    }

    #[test]
    fn calendar_sparse_months_sort_clamp_and_empty() {
        let empty = calendar(&[]);
        assert!(empty.calendar_months().is_empty());
        assert!(empty.calendar_page(usize::MAX).is_none());
        let view = calendar(&["2027-01-02", "2026-08-15", "2026-07-02", "2026-07-01"]);
        assert_eq!(
            view.calendar_months()
                .iter()
                .map(|m| m.label())
                .collect::<Vec<_>>(),
            ["2026 / 07", "2026 / 08", "2027 / 01"]
        );
        let page = view.calendar_page(usize::MAX).unwrap();
        assert_eq!((page.index, page.count), (2, 3));
        assert_eq!(page.month.label(), "2027 / 01");
        assert!(read(&base()).unwrap().calendar_page(0).is_none());
    }

    #[test]
    fn calendar_titles_are_literal_ordered_bounded_and_always_shown() {
        for count in [0usize, 1, 3, 4, 100] {
            let titles = ["same", "same", "", "<script>[[x]]</script>"];
            let view = read(&json!({"layout":"calendar","showTitle":false,"columns":["due"],"groups":[
                {"name":"2026-07-02","rows":(0..count).map(|i| json!({"title":titles[i%4],
                    "cells":["1999-01-01T09:00"],"target":"javascript:alert(1)","cover":"https://invalid/pixel"})).collect::<Vec<_>>()}
            ]})).unwrap();
            let page = view.calendar_page(0).unwrap();
            let day = page
                .cells
                .iter()
                .flatten()
                .find(|day| day.day == 2)
                .unwrap();
            assert_eq!(day.group_index, Some(0));
            assert_eq!(day.titles, titles[..count.min(3)]);
            assert_eq!(day.excess, count.saturating_sub(3));
            assert_eq!(page.month.label(), "2026 / 07");
            assert!(
                page.cells
                    .iter()
                    .flatten()
                    .filter(|day| day.day != 2)
                    .all(|day| day.group_index.is_none() && day.titles.is_empty())
            );
        }
        let view = read(
            &json!({"layout":"calendar","columns":[],"groups":[{"name":"2026-07-01",
            "rows":[{"title":"<script>[[x]]</script>","cells":[]}]}]}),
        )
        .unwrap();
        assert_eq!(
            view.calendar_page(0)
                .unwrap()
                .cells
                .iter()
                .flatten()
                .next()
                .unwrap()
                .titles,
            ["<script>[[x]]</script>"]
        );
    }

    #[test]
    fn calendar_128_months_are_reachable_with_one_bounded_grid() {
        let dates = (1..=128)
            .map(|year| format!("{year:04}-08-01"))
            .collect::<Vec<_>>();
        let view = calendar(&dates.iter().map(String::as_str).collect::<Vec<_>>());
        assert_eq!(view.calendar_months().len(), 128);
        for index in 0..128 {
            let page = view.calendar_page(index).unwrap();
            assert_eq!(
                (page.index, page.count, page.month.year),
                (index, 128, index as u16 + 1)
            );
            assert!(page.cells.len() <= 42);
            assert!(
                page.cells
                    .iter()
                    .flatten()
                    .map(|day| day.titles.len())
                    .sum::<usize>()
                    <= 93
            );
        }
        let groups = (1..=129)
            .map(|year| json!({"name":format!("{year:04}-01-01"),"rows":[]}))
            .collect::<Vec<_>>();
        assert!(
            read(&json!({"layout":"calendar","columns":[],"groups":groups}))
                .unwrap_err()
                .contains("128 groups")
        );
    }
    #[test]
    fn calendar_dense_month_never_projects_more_than_93_titles() {
        let groups = (1..=31)
            .map(|day| {
                json!({"name":format!("2026-08-{day:02}"),
            "rows":vec![json!({"title":"same","cells":[]});4]})
            })
            .collect::<Vec<_>>();
        let view = read(&json!({"layout":"calendar","columns":[],"groups":groups})).unwrap();
        let page = view.calendar_page(0).unwrap();
        assert_eq!(page.cells.len(), 42);
        assert_eq!(
            page.cells
                .iter()
                .flatten()
                .map(|day| day.titles.len())
                .sum::<usize>(),
            93
        );
        assert_eq!(
            page.cells
                .iter()
                .flatten()
                .map(|day| day.excess)
                .sum::<usize>(),
            31
        );
        assert_eq!(view.total_rows(), 124);
    }
}
