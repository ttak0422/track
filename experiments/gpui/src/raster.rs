//! Bounded raster-only display. No paths are opened or URLs fetched here.
//! Reference validation is lexical; the serving engine owns filesystem/symlink policy.
use image::{ImageDecoder, ImageFormat, ImageReader};
use std::{io::Cursor, sync::Arc};

pub const MAX_ENCODED: usize = 4 * 1024 * 1024;
pub const MAX_DIMENSION: u32 = 2048;
pub const MAX_PIXELS: u64 = 4_000_000;
pub const THUMBNAIL_WIDTH: u32 = 320;
pub const THUMBNAIL_HEIGHT: u32 = 180;
pub const MAX_THUMBNAIL_BYTES: usize = 320 * 180 * 4;

/// One coupled size constraint: height follows the natural ratio after the
/// layout engine constrains width to the reader's available width.
/// Decoded dimensions are nonzero; the defensive fallback avoids invalid style.
pub fn inline_geometry(width: u32, height: u32) -> (f32, f32) {
    if width == 0 || height == 0 {
        return (0., 1.);
    }
    let ratio = width as f32 / height as f32;
    ((width as f32).min(620.).min(480. * ratio), ratio)
}

pub struct Decoded {
    pub image: Arc<gpui::RenderImage>,
    pub width: u32,
    pub height: u32,
}

impl Decoded {
    /// Retained single-frame BGRA payload, excluding small image bookkeeping.
    pub fn byte_len(&self) -> usize {
        self.width as usize * self.height as usize * 4
    }
}

pub fn asset_name(reference: &str) -> Result<&str, String> {
    let invalid = || "Only literal assets/ PNG or JPEG references are supported.".to_owned();
    let name = reference.strip_prefix("assets/").ok_or_else(invalid)?;
    if reference.len() > 4096
        || name.is_empty()
        || name
            .chars()
            .any(|c| c.is_control() || matches!(c, '\\' | '%' | '?' | '#' | ':'))
        || name
            .split('/')
            .any(|part| part.is_empty() || matches!(part, "." | "..") || part.trim() != part)
        || !name.rsplit_once('.').is_some_and(|(_, ext)| {
            matches!(ext.to_ascii_lowercase().as_str(), "png" | "jpg" | "jpeg")
        })
    {
        return Err(invalid());
    }
    Ok(name)
}

fn reader(bytes: &[u8], format: ImageFormat) -> ImageReader<Cursor<&[u8]>> {
    let mut reader = ImageReader::with_format(Cursor::new(bytes), format);
    let mut limits = image::Limits::default();
    limits.max_image_width = Some(MAX_DIMENSION);
    limits.max_image_height = Some(MAX_DIMENSION);
    // The decoder's allocation accounting is not a hard process RSS bound.
    limits.max_alloc = Some(16 * 1024 * 1024);
    reader.limits(limits);
    reader
}

pub fn decode(bytes: &[u8], content_type: &str) -> Result<Decoded, String> {
    decode_variant(bytes, content_type, false)
}

/// Decode and resize on the caller's owned worker, never the UI thread.
/// Source dimensions and allocation limits are enforced before downsampling.
pub fn decode_thumbnail(bytes: &[u8], content_type: &str) -> Result<Decoded, String> {
    decode_variant(bytes, content_type, true)
}

fn decode_variant(bytes: &[u8], content_type: &str, thumbnail: bool) -> Result<Decoded, String> {
    if bytes.len() > MAX_ENCODED {
        return Err("Image exceeds the encoded byte limit.".into());
    }
    let mime = content_type.split(';').next().unwrap_or("").trim();
    let expected = if mime.eq_ignore_ascii_case("image/png") {
        ImageFormat::Png
    } else if mime.eq_ignore_ascii_case("image/jpeg") {
        ImageFormat::Jpeg
    } else {
        return Err("Only PNG and JPEG content types are supported.".into());
    };
    let format = image::guess_format(bytes).map_err(|_| "Unknown raster signature.".to_owned())?;
    if format != expected {
        return Err("Image signature and content type disagree.".into());
    }
    // Metadata is read separately so pixel decoding keeps ImageReader::decode's
    // allocation reservation and limits, rather than bypassing them via from_decoder.
    let (width, height, orientation) = {
        let mut header = reader(bytes, format)
            .into_decoder()
            .map_err(|e| e.to_string())?;
        let (width, height) = header.dimensions();
        let orientation = header.orientation().map_err(|e| e.to_string())?;
        (width, height, orientation)
    };
    if width == 0
        || height == 0
        || width > MAX_DIMENSION
        || height > MAX_DIMENSION
        || u64::from(width) * u64::from(height) > MAX_PIXELS
    {
        return Err("Image dimensions exceed the display limit.".into());
    }
    let mut decoded = reader(bytes, format).decode().map_err(|e| e.to_string())?;
    if (decoded.width(), decoded.height()) != (width, height) {
        return Err("Image dimensions changed during decode.".into());
    }
    // Orientation changes presentation, not source admission. Apply before
    // thumbnail aspect calculations; RGBA/BGRA channel and alpha policy is unchanged.
    decoded.apply_orientation(orientation);
    let mut buffer = decoded.into_rgba8();
    let (width, height) = buffer.dimensions();
    if thumbnail && (width > THUMBNAIL_WIDTH || height > THUMBNAIL_HEIGHT) {
        // Consume the full buffer here: only the bounded downsample survives into
        // RenderImage. Integer dimensions preserve aspect to pixel rounding.
        let full = image::DynamicImage::ImageRgba8(buffer);
        buffer = full
            .thumbnail(THUMBNAIL_WIDTH, THUMBNAIL_HEIGHT)
            .into_rgba8();
        drop(full);
    }
    let (width, height) = buffer.dimensions();
    if thumbnail && buffer.len() > MAX_THUMBNAIL_BYTES {
        return Err("Thumbnail exceeds the retained byte limit.".into());
    }
    for pixel in buffer.chunks_exact_mut(4) {
        pixel.swap(0, 2);
    }
    Ok(Decoded {
        image: Arc::new(gpui::RenderImage::new([image::Frame::new(buffer)])),
        width,
        height,
    })
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    pub fn png(width: u32, height: u32) -> Vec<u8> {
        use image::ImageEncoder;
        let mut bytes = Vec::new();
        image::codecs::png::PngEncoder::new(&mut bytes)
            .write_image(
                &vec![0x20; width as usize * height as usize * 4],
                width,
                height,
                image::ExtendedColorType::Rgba8,
            )
            .unwrap();
        bytes
    }
    #[test]
    fn paths_are_lexical_local_and_never_urls_or_traversal() {
        assert_eq!(
            asset_name("assets/図/hello world.PNG").unwrap(),
            "図/hello world.PNG"
        );
        for path in [
            "https://a/a.png",
            "file:///a.png",
            "/assets/a.png",
            "assets//a.png",
            "assets/../a.png",
            "assets/./a.png",
            "assets/a%2f.png",
            "assets/a.png?x=1",
            "assets/a.png#x",
            "assets/a\\b.png",
            "assets/a.svg",
            "assets/a.gif",
            "assets/ a.png",
            "assets/a\0.png",
        ] {
            assert!(asset_name(path).is_err(), "{path}");
        }
    }
    #[test]
    fn png_and_jpeg_decode_without_gpui_resource_loading() {
        use image::ImageEncoder;
        let mut bytes = Vec::new();
        image::codecs::png::PngEncoder::new(&mut bytes)
            .write_image(&[1, 2, 3, 255], 1, 1, image::ExtendedColorType::Rgba8)
            .unwrap();
        let decoded = decode(&bytes, "image/png; charset=binary").unwrap();
        assert_eq!((decoded.width, decoded.height), (1, 1));
        assert_eq!(decoded.image.as_bytes(0).unwrap(), &[3, 2, 1, 255]);
        let mut jpeg = Vec::new();
        image::codecs::jpeg::JpegEncoder::new(&mut jpeg)
            .encode(&[10, 20, 30], 1, 1, image::ExtendedColorType::Rgb8)
            .unwrap();
        assert_eq!(decode(&jpeg, "image/jpeg").unwrap().width, 1);
    }
    pub(crate) fn jpeg(width: u32, height: u32) -> Vec<u8> {
        let pixels: Vec<u8> = (0..width * height)
            .flat_map(|i| [(i * 37) as u8, (i * 53) as u8, (i * 79) as u8])
            .collect();
        let mut bytes = Vec::new();
        image::codecs::jpeg::JpegEncoder::new_with_quality(&mut bytes, 100)
            .encode(&pixels, width, height, image::ExtendedColorType::Rgb8)
            .unwrap();
        bytes
    }
    pub(crate) fn with_exif(jpeg: &[u8], orientation: u16, malformed: bool) -> Vec<u8> {
        // JPEG APP1 contains Exif + little-endian TIFF, one SHORT orientation tag.
        let mut payload = b"Exif\0\0II\x2a\0\x08\0\0\0\x01\0\x12\x01\x03\0\x01\0\0\0".to_vec();
        payload.extend_from_slice(&orientation.to_le_bytes());
        payload.extend_from_slice(&[0; 6]);
        if malformed {
            payload.truncate(10);
        }
        let mut result = jpeg[..2].to_vec();
        result.extend_from_slice(&[0xff, 0xe1]);
        result.extend_from_slice(&((payload.len() + 2) as u16).to_be_bytes());
        result.extend_from_slice(&payload);
        result.extend_from_slice(&jpeg[2..]);
        result
    }
    #[test]
    fn jpeg_exif_all_orientations_preserve_exact_decoded_pixel_mapping() {
        let bytes = jpeg(3, 2);
        let baseline = decode(&bytes, "image/jpeg").unwrap();
        let pixels = baseline.image.as_bytes(0).unwrap();
        for orientation in 1..=8 {
            let image = decode(&with_exif(&bytes, orientation, false), "image/jpeg").unwrap();
            let (w, h) = if orientation >= 5 { (2, 3) } else { (3, 2) };
            assert_eq!((image.width, image.height), (w, h));
            let mut expected = vec![0; 24];
            for y in 0..2usize {
                for x in 0..3usize {
                    let (dx, dy) = match orientation {
                        1 => (x, y),
                        2 => (2 - x, y),
                        3 => (2 - x, 1 - y),
                        4 => (x, 1 - y),
                        5 => (y, x),
                        6 => (1 - y, x),
                        7 => (1 - y, 2 - x),
                        8 => (y, 2 - x),
                        _ => unreachable!(),
                    };
                    let src = (y * 3 + x) * 4;
                    let dest = (dy * w as usize + dx) * 4;
                    expected[dest..dest + 4].copy_from_slice(&pixels[src..src + 4]);
                }
            }
            assert_eq!(
                image.image.as_bytes(0).unwrap(),
                expected.as_slice(),
                "orientation {orientation}"
            );
        }
        for (orientation, malformed) in [(0, false), (9, false), (6, true)] {
            let image = decode(&with_exif(&bytes, orientation, malformed), "image/jpeg").unwrap();
            assert_eq!((image.width, image.height), (3, 2));
            assert_eq!(image.image.as_bytes(0).unwrap(), pixels);
        }
    }
    #[test]
    fn jpeg_orientation_precedes_thumbnail_geometry() {
        let bytes = jpeg(640, 360);
        let plain = decode_thumbnail(&bytes, "image/jpeg").unwrap();
        assert_eq!((plain.width, plain.height), (320, 180));
        for orientation in 5..=8 {
            let image =
                decode_thumbnail(&with_exif(&bytes, orientation, false), "image/jpeg").unwrap();
            assert_eq!((image.width, image.height), (101, 180));
            assert!(image.byte_len() <= MAX_THUMBNAIL_BYTES);
        }
    }
    #[test]
    fn raster_limits_reject_wrong_format_corruption_and_large_dimensions() {
        let bytes = png(1, 1);
        for mime in ["image/svg+xml", "text/html", "image/gif", "image/jpeg", ""] {
            assert!(decode(&bytes, mime).is_err());
        }
        for bad in [b"<svg/>".as_slice(), b"GIF89a", &bytes[..12]] {
            assert!(decode(bad, "image/png").is_err());
        }
        assert!(decode(&vec![0; MAX_ENCODED + 1], "image/png").is_err());
        assert!(decode(&png(2049, 1), "image/png").is_err());
        assert!(decode(&png(2001, 2000), "image/png").is_err());
    }
    #[test]
    fn thumbnails_bound_retained_pixels_for_png_and_jpeg() {
        for (width, height, expected) in [
            (640, 360, (320, 180)),
            (1000, 1000, (180, 180)),
            (100, 1000, (18, 180)),
            (1000, 100, (320, 32)),
            (1, 2048, (1, 180)),
            (2, 1, (2, 1)),
        ] {
            let mut jpeg = Vec::new();
            image::codecs::jpeg::JpegEncoder::new(&mut jpeg)
                .encode(
                    &vec![32; width as usize * height as usize * 3],
                    width,
                    height,
                    image::ExtendedColorType::Rgb8,
                )
                .unwrap();
            for (bytes, mime) in [(png(width, height), "image/png"), (jpeg, "image/jpeg")] {
                let thumb = decode_thumbnail(&bytes, mime).unwrap();
                assert_eq!((thumb.width, thumb.height), expected);
                assert_eq!(thumb.byte_len(), thumb.image.as_bytes(0).unwrap().len());
                assert!(thumb.byte_len() <= MAX_THUMBNAIL_BYTES);
                assert!(thumb.image.as_bytes(1).is_none());
            }
        }
        let bytes = png(640, 360);
        let full = decode(&bytes, "image/png").unwrap();
        assert_eq!((full.width, full.height), (640, 360));
        assert_eq!(full.byte_len(), 640 * 360 * 4);
        assert_eq!(32 * MAX_THUMBNAIL_BYTES, 7_372_800);
        assert!(32 * MAX_THUMBNAIL_BYTES <= 8 * 1024 * 1024);
    }

    #[test]
    fn thumbnail_downsampling_never_bypasses_source_limits() {
        for (width, height) in [(2049, 1), (2001, 2000)] {
            assert!(decode_thumbnail(&png(width, height), "image/png").is_err());
            let mut jpeg = Vec::new();
            image::codecs::jpeg::JpegEncoder::new(&mut jpeg)
                .encode(
                    &vec![32; width as usize * height as usize * 3],
                    width,
                    height,
                    image::ExtendedColorType::Rgb8,
                )
                .unwrap();
            assert!(decode_thumbnail(&jpeg, "image/jpeg").is_err());
        }
        let bytes = png(1, 1);
        for mime in ["image/gif", "image/jpeg", "text/html", ""] {
            assert!(decode_thumbnail(&bytes, mime).is_err());
        }
        assert!(decode_thumbnail(&bytes[..12], "image/png").is_err());
        assert!(decode_thumbnail(&vec![0; MAX_ENCODED + 1], "image/png").is_err());
    }
}

#[cfg(test)]
mod inline_geometry_tests {
    use super::inline_geometry;

    #[test]
    fn inline_width_and_ratio_preserve_portrait_landscape_and_small_images() {
        for (width, height, expected_width) in [
            (600, 1200, 240.),
            (1200, 600, 620.),
            (1000, 1000, 480.),
            (100, 200, 100.),
            (1, 2048, 480. / 2048.),
            (2048, 1, 620.),
        ] {
            let (cap, ratio) = inline_geometry(width, height);
            assert!((cap - expected_width).abs() < 0.001);
            for parent_width in [1_f32, 80., 200., 400., 1000.] {
                // The parent frame uses this cap with max_w_full and aspect ratio.
                let rendered_width = cap.min(parent_width);
                let rendered_height = rendered_width / ratio;
                assert!(rendered_width <= parent_width && rendered_width <= width as f32);
                assert!(rendered_height <= height as f32 + 0.001);
                assert!(rendered_height <= 480.001);
                assert!((rendered_width / rendered_height - ratio).abs() < 0.001);
            }
        }
    }

    #[test]
    fn invalid_inline_dimensions_do_not_produce_nan_or_infinite_styles() {
        for dimensions in [(0, 0), (0, 100), (100, 0)] {
            assert_eq!(inline_geometry(dimensions.0, dimensions.1), (0., 1.));
        }
    }
}
