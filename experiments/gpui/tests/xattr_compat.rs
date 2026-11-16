//! Real filesystem and tar regression tests for the explicit xattr source patch.
#![cfg(target_os = "linux")]
use std::{
    fs::{self, File},
    path::PathBuf,
    time::{SystemTime, UNIX_EPOCH},
};
use xattr::FileExt;

struct Fixture(PathBuf);
impl Fixture {
    fn new() -> Self {
        let path = std::env::temp_dir().join(format!(
            "track-xattr-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir(&path).unwrap();
        Self(path)
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        fs::remove_dir_all(&self.0).unwrap();
    }
}

#[test]
fn path_and_fd_attributes_preserve_missing_and_other_errors() {
    let fixture = Fixture::new();
    let path = fixture.0.join("file");
    let file = File::create(&path).unwrap();
    let name = "user.track-test";
    assert_eq!(xattr::get(&path, name).unwrap(), None);
    assert_eq!(file.get_xattr(name).unwrap(), None);
    xattr::set(&path, name, b"path").unwrap();
    assert_eq!(xattr::get(&path, name).unwrap(), Some(b"path".to_vec()));
    assert_eq!(file.get_xattr(name).unwrap(), Some(b"path".to_vec()));
    assert!(xattr::list(&path).unwrap().any(|key| key == name));
    assert!(file.list_xattr().unwrap().any(|key| key == name));
    xattr::remove(&path, name).unwrap();
    assert_eq!(file.get_xattr(name).unwrap(), None);
    file.set_xattr(name, b"fd").unwrap();
    assert_eq!(xattr::get(&path, name).unwrap(), Some(b"fd".to_vec()));
    file.remove_xattr(name).unwrap();
    assert_eq!(xattr::get(&path, name).unwrap(), None);
    assert_eq!(file.get_xattr(name).unwrap(), None);
    assert!(!file.list_xattr().unwrap().any(|key| key == name));
    assert!(!xattr::list(&path).unwrap().any(|key| key == name));
    // ENOENT must remain an error, not be confused with a missing attribute.
    assert_eq!(
        xattr::get(fixture.0.join("absent"), name)
            .unwrap_err()
            .raw_os_error(),
        Some(libc::ENOENT)
    );
    assert!(file.get_xattr("invalid\0name").is_err());
}

#[test]
fn tar_feature_still_restores_pax_extended_attributes() {
    async_std::task::block_on(async {
        let fixture = Fixture::new();
        let mut builder = zed_async_tar::Builder::new(Vec::new());
        let record = "SCHILY.xattr.user.track-test=preserved\n";
        let mut size = record.len() + 3;
        loop {
            let next = record.len() + size.to_string().len() + 1;
            if next == size {
                break;
            }
            size = next;
        }
        let pax = format!("{size} {record}");
        let mut header = zed_async_tar::Header::new_ustar();
        header.set_entry_type(zed_async_tar::EntryType::XHeader);
        header.set_size(pax.len() as u64);
        header.set_mode(0o600);
        builder
            .append_data(&mut header, "pax", pax.as_bytes())
            .await
            .unwrap();
        let mut header = zed_async_tar::Header::new_ustar();
        header.set_size(4);
        header.set_mode(0o600);
        builder
            .append_data(&mut header, "file", &b"body"[..])
            .await
            .unwrap();
        let bytes = builder.into_inner().await.unwrap();
        // Keep the same enabled xattr capability, not an extraction feature-off workaround.
        let archive = zed_async_tar::ArchiveBuilder::new(bytes.as_slice())
            .set_unpack_xattrs(true)
            .build();
        archive.unpack(&fixture.0).await.unwrap();
        assert_eq!(fs::read(fixture.0.join("file")).unwrap(), b"body");
        assert_eq!(
            xattr::get(fixture.0.join("file"), "user.track-test").unwrap(),
            Some(b"preserved".to_vec())
        );
    });
}
