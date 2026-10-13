use std::{cell::RefCell, ptr, sync::Arc};

use vello_cpu::peniko::{Blob, FontData};
use vello_cpu::{Glyph, Pixmap, RenderContext, Resources};

thread_local! {
    static FONTS: RefCell<Vec<FontData>> = const { RefCell::new(Vec::new()) };
}

// The caller owns a buffer until it passes that buffer to raster_register_font,
// which consumes it. Render output remains caller-owned and must be freed.
#[unsafe(no_mangle)]
pub extern "C" fn raster_alloc(len: usize) -> *mut u8 {
    if len == 0 || len > 64 << 20 {
        return ptr::null_mut();
    }
    Box::into_raw(vec![0_u8; len].into_boxed_slice()) as *mut u8
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn raster_free(ptr: *mut u8, len: usize) {
    if !ptr.is_null() && len != 0 {
        unsafe { drop(Box::from_raw(ptr::slice_from_raw_parts_mut(ptr, len))) }
    }
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn raster_register_font(
    font_ptr: *mut u8,
    font_len: usize,
    face_index: u32,
) -> i32 {
    if font_ptr.is_null() || font_len == 0 || font_len > 64 << 20 {
        return -1;
    }
    let bytes = unsafe { Box::from_raw(ptr::slice_from_raw_parts_mut(font_ptr, font_len)) };
    FONTS.with(|fonts| {
        let mut fonts = fonts.borrow_mut();
        fonts.push(FontData::new(
            Blob::new(Arc::new(bytes.into_vec())),
            face_index,
        ));
        (fonts.len() - 1) as i32
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn raster_glyph(
    font_id: u32,
    glyph_id: u32,
    size: f32,
    x: f32,
    y: f32,
    width: u32,
    height: u32,
    output: *mut u8,
) -> i32 {
    if output.is_null()
        || width == 0
        || height == 0
        || width > 4096
        || height > 4096
        || width as usize * height as usize * 4 > 64 << 20
    {
        return -1;
    }
    FONTS.with(|fonts| {
        let fonts = fonts.borrow();
        let Some(font) = fonts.get(font_id as usize) else {
            return -2;
        };
        let mut resources = Resources::new();
        let mut context = RenderContext::new(width as u16, height as u16);
        if context
            .glyph_run(&mut resources, font)
            .font_size(size)
            .fill_glyphs([Glyph { id: glyph_id, x, y }].into_iter())
            .is_err()
        {
            return -3;
        }
        let mut pixmap = Pixmap::new(width as u16, height as u16);
        context.flush();
        context.render(&mut pixmap, &mut resources);
        let target =
            unsafe { std::slice::from_raw_parts_mut(output, width as usize * height as usize * 4) };
        for (i, pixel) in pixmap.data().iter().enumerate() {
            target[i * 4..i * 4 + 4].copy_from_slice(&pixel.to_u8_array());
        }
        0
    })
}
