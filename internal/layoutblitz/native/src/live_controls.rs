use crate::ffi::{Handle, call};
#[unsafe(no_mangle)]
pub unsafe extern "C" fn mimic_blitz_control_value(
    handle: *mut Handle,
    id: u64,
    data: *const u8,
    len: usize,
    multiline: u32,
) -> i32 {
    call(handle, |owner| {
        let value = if len == 0 {
            ""
        } else {
            if data.is_null() {
                return Err(());
            }
            std::str::from_utf8(unsafe { std::slice::from_raw_parts(data, len) }).map_err(|_| ())?
        };
        let node = owner.node(id).map_err(|_| ())?;
        if owner
            .document
            .get_node(node)
            .and_then(|node| node.element_data())
            .and_then(|element| element.text_input_data())
            .is_some_and(|data| data.is_multiline == (multiline != 0) && data.editor.text() == value)
        {
            return Ok(());
        }
        owner
            .document
            .set_canonical_control_value(node, value, multiline != 0);
        owner.dirty = true;
        Ok(())
    })
}

#[cfg(test)]
mod tests {
    use crate::Owner;
    #[test]
    fn canonical_live_text_keeps_attribute_and_clears() {
        let mut owner = Owner::new(1, 800, 600);
        owner
            .element(2, "http://www.w3.org/1999/xhtml", "input")
            .unwrap();
        owner.append(1, 2).unwrap();
        owner.attribute(2, "", "value", "default").unwrap();
        let id = owner.node(2).unwrap();
        for value in ["Wikipedia JavaScript", "", "default"] {
            owner.document.set_canonical_control_value(id, value, false);
            let element = owner.document.get_node(id).unwrap().element_data().unwrap();
            assert_eq!(element.text_input_data().unwrap().editor.text(), value);
            assert_eq!(
                element.attr(markup5ever::local_name!("value")),
                Some("default")
            );
        }
    }
}
