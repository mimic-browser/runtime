//! ABI calls are serialized by the owning Page. No Go pointers are retained.
use crate::{Owner, Rect};
use std::panic::{AssertUnwindSafe, catch_unwind};

pub struct Handle {
    owner: Owner,
    poisoned: bool,
}

unsafe fn text<'a>(ptr: *const u8, len: usize) -> Result<&'a str, ()> {
    if len == 0 {
        return Ok("");
    }
    if ptr.is_null() {
        return Err(());
    }
    std::str::from_utf8(unsafe { std::slice::from_raw_parts(ptr, len) }).map_err(|_| ())
}

pub(super) fn call(handle: *mut Handle, f: impl FnOnce(&mut Owner) -> Result<(), ()>) -> i32 {
    if handle.is_null() {
        return -1;
    }
    let handle = unsafe { &mut *handle };
    if handle.poisoned {
        return -2;
    }
    match catch_unwind(AssertUnwindSafe(|| f(&mut handle.owner))) {
        Ok(Ok(())) => 0,
        _ => {
            handle.poisoned = true;
            -2
        }
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn mimic_blitz_new(root: u64, width: u32, height: u32) -> *mut Handle {
    catch_unwind(AssertUnwindSafe(|| {
        Box::into_raw(Box::new(Handle {
            owner: Owner::new(root, width, height),
            poisoned: false,
        }))
    }))
    .unwrap_or(std::ptr::null_mut())
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_drop(handle: *mut Handle) {
    if !handle.is_null() {
        drop(unsafe { Box::from_raw(handle) });
    }
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_element(
    handle: *mut Handle,
    id: u64,
    ns: *const u8,
    ns_len: usize,
    name: *const u8,
    name_len: usize,
) -> i32 {
    call(handle, |owner| {
        owner
            .element(id, unsafe { text(ns, ns_len)? }, unsafe {
                text(name, name_len)?
            })
            .map_err(|_| ())
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_attribute(
    handle: *mut Handle,
    id: u64,
    ns: *const u8,
    ns_len: usize,
    name: *const u8,
    name_len: usize,
    value: *const u8,
    value_len: usize,
) -> i32 {
    call(handle, |owner| {
        owner
            .attribute(
                id,
                unsafe { text(ns, ns_len)? },
                unsafe { text(name, name_len)? },
                unsafe { text(value, value_len)? },
            )
            .map_err(|_| ())
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn mimic_blitz_append(handle: *mut Handle, parent: u64, child: u64) -> i32 {
    call(handle, |owner| owner.append(parent, child).map_err(|_| ()))
}

#[unsafe(no_mangle)]
pub extern "C" fn mimic_blitz_color_scheme(handle: *mut Handle, dark: u32) -> i32 {
    call(handle, |owner| {
        owner.color_scheme(dark != 0);
        Ok(())
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn mimic_blitz_state(handle: *mut Handle, id: u64, mask: u32, flags: u32) -> i32 {
    call(handle, |owner| owner.state(id, mask, flags).map_err(|_| ()))
}

#[unsafe(no_mangle)]
pub extern "C" fn mimic_blitz_restyle_subtree(handle: *mut Handle) -> i32 {
    call(handle, |owner| {
        owner.restyle_subtree();
        Ok(())
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn mimic_blitz_detach(handle: *mut Handle, id: u64) -> i32 {
    call(handle, |owner| owner.detach(id).map_err(|_| ()))
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_has_computed_style(
    handle: *mut Handle,
    id: u64,
    output: *mut u32,
) -> i32 {
    if output.is_null() {
        return -1;
    }
    call(handle, |owner| {
        let node = owner.node(id).map_err(|_| ())?;
        unsafe {
            *output = u32::from(
                owner
                    .document
                    .get_node(node)
                    .unwrap()
                    .primary_styles()
                    .is_some(),
            );
        }
        Ok(())
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn mimic_blitz_viewport(handle: *mut Handle, width: u32, height: u32) -> i32 {
    call(handle, |owner| {
        owner.viewport(width, height);
        Ok(())
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_base_url(
    handle: *mut Handle,
    value: *const u8,
    len: usize,
) -> i32 {
    call(handle, |owner| {
        owner.base_url(unsafe { text(value, len)? });
        Ok(())
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_clear_attribute(
    handle: *mut Handle,
    id: u64,
    ns: *const u8,
    ns_len: usize,
    name: *const u8,
    name_len: usize,
) -> i32 {
    call(handle, |owner| {
        owner
            .clear_attribute(id, unsafe { text(ns, ns_len)? }, unsafe {
                text(name, name_len)?
            })
            .map_err(|_| ())
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_stylesheet(
    handle: *mut Handle,
    id: u64,
    value: *const u8,
    len: usize,
) -> i32 {
    call(handle, |owner| {
        owner
            .stylesheet(id, unsafe { text(value, len)? })
            .map_err(|_| ())
    })
}

/// CSS and URL bytes are borrowed only for this parse transaction.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_stylesheet_url(
    handle: *mut Handle,
    id: u64,
    value: *const u8,
    len: usize,
    url: *const u8,
    url_len: usize,
) -> i32 {
    call(handle, |owner| {
        owner
            .stylesheet_at_url(id, unsafe { text(value, len)? }, unsafe {
                text(url, url_len)?
            })
            .map_err(|_| ())
    })
}

/// Output bytes are copied into caller-owned storage. -3 asks for a larger
/// buffer without poisoning the owner; no native allocation crosses the ABI.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_style(
    handle: *mut Handle,
    id: u64,
    name: *const u8,
    len: usize,
    output: *mut u8,
    capacity: usize,
    written: *mut usize,
) -> i32 {
    if written.is_null() || (capacity != 0 && output.is_null()) {
        return -1;
    }
    let mut insufficient = false;
    let status = call(handle, |owner| {
        let value = owner
            .style(id, unsafe { text(name, len)? })
            .map_err(|_| ())?;
        unsafe {
            *written = value.len();
        }
        if capacity < value.len() {
            insufficient = true;
            return Ok(());
        }
        if !value.is_empty() {
            unsafe {
                std::ptr::copy_nonoverlapping(value.as_ptr(), output, value.len());
            }
        }
        Ok(())
    });
    if status == 0 && insufficient {
        -3
    } else {
        status
    }
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_text(
    handle: *mut Handle,
    id: u64,
    value: *const u8,
    len: usize,
    create: u32,
) -> i32 {
    call(handle, |owner| {
        let value = unsafe { text(value, len)? };
        if create == 2 {
            owner.comment(id, value)
        } else if create != 0 {
            owner.text(id, value)
        } else {
            owner.set_text(id, value)
        }
        .map_err(|_| ())
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_resolve(
    handle: *mut Handle,
    time: f64,
    generation: *mut u64,
) -> i32 {
    if generation.is_null() {
        return -1;
    }
    call(handle, |owner| {
        owner.resolve(time);
        unsafe {
            *generation = owner.generation;
        }
        Ok(())
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_rect(handle: *mut Handle, id: u64, output: *mut Rect) -> i32 {
    if output.is_null() {
        return -1;
    }
    call(handle, |owner| {
        let rect = owner.rect(id).map_err(|_| ())?;
        unsafe {
            *output = rect;
        }
        Ok(())
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn invalid_update_poisons_owner_until_disposal() {
        let handle = mimic_blitz_new(1, 800, 600);
        assert!(!handle.is_null());
        assert_eq!(mimic_blitz_append(handle, 1, 999), -2);
        let mut generation = 0;
        assert_eq!(
            unsafe { mimic_blitz_resolve(handle, 0.0, &mut generation) },
            -2
        );
        unsafe {
            mimic_blitz_drop(handle);
        }
    }
}
