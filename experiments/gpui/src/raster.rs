//! Bounded raster-only display. No paths are opened or URLs fetched here.
//! Reference validation is lexical; the serving engine owns filesystem/symlink policy.
use image::{ImageFormat, ImageReader};
use std::{io::Cursor, sync::Arc};

pub const MAX_ENCODED: usize = 4 * 1024 * 1024;
pub const MAX_DIMENSION: u32 = 2048;
pub const MAX_PIXELS: u64 = 4_000_000;
pub const THUMBNAIL_WIDTH: u32 = 320;
pub const THUMBNAIL_HEIGHT: u32 = 180;
pub const MAX_THUMBNAIL_BYTES: usize = 320 * 180 * 4;

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
    let (width, height) = reader(bytes, format)
        .into_dimensions()
        .map_err(|e| e.to_string())?;
    if width == 0
        || height == 0
        || width > MAX_DIMENSION
        || height > MAX_DIMENSION
        || u64::from(width) * u64::from(height) > MAX_PIXELS
    {
        return Err("Image dimensions exceed the display limit.".into());
    }
    let mut buffer = reader(bytes, format)
        .decode()
        .map_err(|e| e.to_string())?
        .into_rgba8();
    if buffer.dimensions() != (width, height) {
        return Err("Image dimensions changed during decode.".into());
    }
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
