//! Real Go asset transport and native raster decoding, without GUI rendering.
use super::*;

fn solid_png(rgba: [u8; 4]) -> Vec<u8> {
    use image::ImageEncoder;
    let mut bytes = Vec::new();
    image::codecs::png::PngEncoder::new(&mut bytes)
        .write_image(
            &rgba.repeat(400 * 200),
            400,
            200,
            image::ExtendedColorType::Rgba8,
        )
        .unwrap();
    bytes
}

#[test]
#[ignore = "requires an explicitly supplied synthetic Go bridge binary"]
fn bound_assets_keep_vault_pixels_and_reject_fallbacks() {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").expect("explicit test bridge"),
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    for (vault, rgba) in [("main", [255, 0, 0, 255]), ("work", [0, 0, 255, 255])] {
        let assets = fixture.0.join("home").join(vault).join("assets");
        std::fs::create_dir_all(&assets).unwrap();
        std::fs::write(assets.join("same.png"), solid_png(rgba)).unwrap();
        let oriented =
            crate::raster::tests::with_exif(&crate::raster::tests::jpeg(400, 200), 6, false);
        std::fs::write(assets.join("oriented.jpg"), oriented).unwrap();
        std::fs::write(assets.join("not-image.png"), b"plain text, not pixels\n").unwrap();
        if vault == "main" {
            std::fs::write(assets.join("main-only.png"), solid_png(rgba)).unwrap();
        }
    }
    let before = admission_process_tests::protected_inventory(&fixture.0);
    let bound = SyntheticBound::new(proof, fixture.0.clone()).unwrap();
    let owner = OwnedServer::start_admitted(
        bound.proof.launch(),
        &AtomicBool::new(false),
        StartupAdmission::SyntheticBound(bound),
    )
    .unwrap();
    let session = Session::from_owned(owner);
    let client = session.client();
    for (vault, bgra) in [
        ("main", [0, 0, 255, 255]),
        ("work", [255, 0, 0, 255]),
        ("", [255, 0, 0, 255]),
    ] {
        let full = client.raster_asset("assets/same.png", vault).unwrap();
        assert_eq!((full.width, full.height), (400, 200));
        assert_eq!(full.image.as_bytes(0).unwrap(), bgra.repeat(400 * 200));
        let thumb = client.raster_thumbnail("assets/same.png", vault).unwrap();
        assert_eq!((thumb.width, thumb.height), (320, 160));
        assert_eq!(thumb.image.as_bytes(0).unwrap(), bgra.repeat(320 * 160));
        assert!(thumb.byte_len() <= crate::raster::MAX_THUMBNAIL_BYTES);
        // EXIF orientation must be applied before thumbnail sizing, including
        // the real engine's JPEG content type and scoped HTTP transport.
        let oriented = client.raster_asset("assets/oriented.jpg", vault).unwrap();
        assert_eq!((oriented.width, oriented.height), (200, 400));
        let oriented_thumb = client
            .raster_thumbnail("assets/oriented.jpg", vault)
            .unwrap();
        assert_eq!((oriented_thumb.width, oriented_thumb.height), (90, 180));
        assert!(oriented_thumb.byte_len() <= crate::raster::MAX_THUMBNAIL_BYTES);
        for thumbnail in [false, true] {
            let read = |name| {
                if thumbnail {
                    client.raster_thumbnail(name, vault)
                } else {
                    client.raster_asset(name, vault)
                }
            };
            assert!(matches!(read("assets/not-image.png"), Err(Error::Shape(_))));
            assert!(matches!(
                read("assets/missing.png"),
                Err(Error::Http { status: 404, .. })
            ));
            assert!(matches!(read("assets/../same.png"), Err(Error::Config(_))));
            assert!(matches!(
                read("assets/%2e%2e/same.png"),
                Err(Error::Config(_))
            ));
        }
    }
    // A file present in main must not be found through the selected work or
    // unnamed launch scope; invalid registry scopes cannot fall back either.
    for vault in ["work", ""] {
        assert!(matches!(
            client.raster_asset("assets/main-only.png", vault),
            Err(Error::Http { status: 404, .. })
        ));
        assert!(matches!(
            client.raster_thumbnail("assets/main-only.png", vault),
            Err(Error::Http { status: 404, .. })
        ));
    }
    assert!(
        client
            .raster_asset("assets/same.png", "missing-vault")
            .is_err()
    );
    assert!(
        client
            .raster_thumbnail("assets/same.png", "missing-vault")
            .is_err()
    );
    assert!(session.events().unavailable);
    session.shutdown().unwrap();
    assert!(!client.is_alive());
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0)
    );
}
