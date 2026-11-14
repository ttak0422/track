//! Inert, bounded projection of engine-produced query snapshots.
use serde_json::{Map, Value};

pub const LIST_PAGE: usize = 100;
pub const LANE_PAGE: usize = 4;
pub const CARD_PAGE: usize = 50;
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
    pub cells: Vec<String>,
}

impl View {
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
/// Opaque navigation/asset fields are validated then discarded, never interpreted.
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
        Some(_) => {
            return Err("Unsupported query layout (only list and board are supported)".into());
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
    for group in groups {
        let group = object(group, "group")?;
        optional_string(group, "name", MAX_LABEL_BYTES)?;
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
        for layout in ["list", "board"] {
            let view = read(&json!({"layout":layout,"columns":[],"groups":[],"future":{"x":null}}))
                .unwrap();
            assert!(view.show_title);
            assert_eq!(view.total_rows(), 0);
        }
        assert!(decode("not json").is_err());
        for layout in ["gallery", "calendar", "LIST", ""] {
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
}
