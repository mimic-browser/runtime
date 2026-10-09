//! Persistent derived document. Canonical IDs come from the Go DOM owner.
//! No HTML serialization or JavaScript producer participates in updates.
use blitz_dom::{BaseDocument, DocumentConfig, NodeId, StyleThreading};
use blitz_traits::shell::{ColorScheme, Viewport};
use markup5ever::{LocalName, Namespace, QualName};
use std::collections::HashMap;
use style_dom::ElementState;

mod ffi;
mod fonts;
mod images;
mod live_controls;
mod style_batch;
mod transforms;

// Export the legacy fallback through the same archive so the executable has
// one Rust runtime during migration.
pub use mimic_taffy_layout::mimic_taffy_layout;

pub struct Owner {
    document: BaseDocument,
    nodes: HashMap<u64, NodeId>,
    dirty: bool,
    pub generation: u64,
    color_scheme: ColorScheme,
}

#[derive(Debug, PartialEq)]
pub enum Error {
    UnknownNode(u64),
    DuplicateNode(u64),
    DirtyRead,
    InvalidTopology,
    InvalidStylesheetURL,
}

#[derive(Debug, Copy, Clone, Default)]
#[repr(C)]
pub struct Rect {
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
    pub client_width: f32,
    pub client_height: f32,
    pub content_width: f32,
    pub content_height: f32,
    pub flags: u32,
}

impl Owner {
    pub fn new(root: u64, width: u32, height: u32) -> Self {
        // Parsing is process policy, not mutable Page state. The pinned Stylo
        // defaults disable :has(); reverse invalidation is handled by Mimic's
        // subtree restyle on changed epochs until native dependencies mature.
        static SELECTOR_POLICY: std::sync::Once = std::sync::Once::new();
        SELECTOR_POLICY.call_once(|| {
            stylo_static_prefs::set_pref!("layout.css.has-selector.enabled", true);
        });
        let document = BaseDocument::new(DocumentConfig {
            viewport: Some(Viewport::new(width, height, 1.0, ColorScheme::Light)),
            style_threading: StyleThreading::Sequential,
            ..Default::default()
        });
        let root_node = document.root_node().id;
        Self {
            document,
            nodes: HashMap::from([(root, root_node)]),
            dirty: true,
            generation: 0,
            color_scheme: ColorScheme::Light,
        }
    }

    fn node(&self, id: u64) -> Result<NodeId, Error> {
        self.nodes.get(&id).copied().ok_or(Error::UnknownNode(id))
    }

    pub fn element(&mut self, id: u64, namespace: &str, name: &str) -> Result<(), Error> {
        if self.nodes.contains_key(&id) {
            return Err(Error::DuplicateNode(id));
        }
        let name = QualName::new(None, Namespace::from(namespace), LocalName::from(name));
        let node = self.document.mutate().create_element(name, Vec::new());
        self.nodes.insert(id, node);
        self.dirty = true;
        Ok(())
    }

    pub fn text(&mut self, id: u64, value: &str) -> Result<(), Error> {
        if self.nodes.contains_key(&id) {
            return Err(Error::DuplicateNode(id));
        }
        let node = self.document.mutate().create_text_node(value);
        self.nodes.insert(id, node);
        self.dirty = true;
        Ok(())
    }

    pub fn append(&mut self, parent: u64, child: u64) -> Result<(), Error> {
        let parent = self.node(parent)?;
        let child = self.node(child)?;
        let mut ancestor = Some(parent);
        while let Some(id) = ancestor {
            if id == child {
                return Err(Error::InvalidTopology);
            }
            ancestor = self.document.get_node(id).and_then(|node| node.parent);
        }
        self.document.mutate().append_children(parent, &[child]);
        self.dirty = true;
        Ok(())
    }

    pub fn attribute(
        &mut self,
        id: u64,
        namespace: &str,
        name: &str,
        value: &str,
    ) -> Result<(), Error> {
        let node = self.node(id)?;
        let name = QualName::new(None, Namespace::from(namespace), LocalName::from(name));
        self.document.mutate().set_attribute(node, name, value);
        self.dirty = true;
        Ok(())
    }

    pub fn set_text(&mut self, id: u64, value: &str) -> Result<(), Error> {
        let node = self.node(id)?;
        self.document.mutate().set_node_text(node, value);
        self.dirty = true;
        Ok(())
    }

    pub fn comment(&mut self, id: u64, value: &str) -> Result<(), Error> {
        if self.nodes.contains_key(&id) {
            return Err(Error::DuplicateNode(id));
        }
        let node = self.document.mutate().create_comment_node(value);
        self.nodes.insert(id, node);
        self.dirty = true;
        Ok(())
    }

    pub fn detach(&mut self, id: u64) -> Result<(), Error> {
        let node = self.node(id)?;
        self.document.mutate().remove_node(node);
        self.dirty = true;
        Ok(())
    }

    pub fn clear_attribute(&mut self, id: u64, ns: &str, name: &str) -> Result<(), Error> {
        let node = self.node(id)?;
        self.document.mutate().clear_attribute(
            node,
            QualName::new(None, Namespace::from(ns), LocalName::from(name)),
        );
        self.dirty = true;
        Ok(())
    }

    pub fn stylesheet(&mut self, id: u64, css: &str) -> Result<(), Error> {
        let node = self.node(id)?;
        let sheet = self
            .document
            .make_stylesheet(css, style::stylesheets::Origin::Author);
        self.document.add_stylesheet_for_node(sheet, node);
        self.dirty = true;
        Ok(())
    }

    pub fn stylesheet_at_url(&mut self, id: u64, css: &str, url: &str) -> Result<(), Error> {
        let node = self.node(id)?;
        let sheet = self
            .document
            .make_stylesheet_with_url(css, style::stylesheets::Origin::Author, url)
            .map_err(|_| Error::InvalidStylesheetURL)?;
        self.document.add_stylesheet_for_node(sheet, node);
        self.dirty = true;
        Ok(())
    }

    pub fn viewport(&mut self, width: u32, height: u32) {
        self.document
            .set_viewport(Viewport::new(width, height, 1.0, self.color_scheme));
        self.dirty = true;
    }

    pub fn color_scheme(&mut self, dark: bool) {
        let scheme = if dark {
            ColorScheme::Dark
        } else {
            ColorScheme::Light
        };
        if self.color_scheme != scheme {
            self.color_scheme = scheme;
            let mut viewport = self.document.viewport().clone();
            viewport.color_scheme = scheme;
            self.document.set_viewport(viewport);
            self.dirty = true;
        }
    }

    /// These bits are supplied by the existing canonical input state owner,
    /// never synthesized by rewriting DOM attributes.
    pub fn state(&mut self, id: u64, mask: u32, flags: u32) -> Result<(), Error> {
        let node = self.node(id)?;
        let states = [
            ElementState::CHECKED,
            ElementState::FOCUS,
            ElementState::FOCUSRING,
            ElementState::FOCUS_WITHIN,
            ElementState::URLTARGET,
        ];
        let mut affected = ElementState::empty();
        let mut value = ElementState::empty();
        for (index, state) in states.into_iter().enumerate() {
            if mask & (1 << index) != 0 {
                affected |= state;
            }
            if flags & (1 << index) != 0 {
                value |= state;
            }
        }
        let old = *self.document.get_node(node).unwrap().element_state();
        let next = (old - affected) | (value & affected);
        if old != next {
            if self.document.style_depends_on_state(affected) {
                // A later stylesheet invalidation may inspect class/id on this
                // snapshot. Blitz's state-only snapshot has no attributes, so
                // Stylo's selector lookup would panic even when they did not
                // change. Preserve the complete pre-mutation element here.
                self.document.snapshot_node(node);
            }
            self.document.snapshot_node_and(node, affected, |node| {
                *node.element_state_mut() = next;
                node.mark_ancestors_dirty();
            });
            self.dirty = true;
        }
        Ok(())
    }

    pub fn base_url(&mut self, url: &str) {
        self.document.set_base_url(url);
        self.dirty = true;
    }

    /// Resolve once per dirty transaction. Clean observations never traverse DOM.
    /// Animated documents must explicitly request a new lifecycle update.
    pub fn resolve(&mut self, time: f64) -> bool {
        if !self.dirty {
            return false;
        }
        self.document.resolve(time);
        self.dirty = false;
        self.generation += 1;
        true
    }

    /// Keep parsed sheets, fonts and canonical node identities while rematching
    /// relational selectors whose reverse dependencies are not yet incremental.
    pub fn restyle_subtree(&mut self) {
        if let Some(root) = self.document.try_root_element().map(|node| node.id) {
            self.document.get_node_mut(root).unwrap().set_restyle_hint(
                style::invalidation::element::restyle_hints::RestyleHint::restyle_subtree(),
            );
            self.dirty = true;
        }
    }

    pub fn rect(&self, id: u64) -> Result<Rect, Error> {
        if self.dirty {
            return Err(Error::DirtyRead);
        }
        let node = self.node(id)?;
        if !self.document.get_node(node).unwrap().has_boxes() {
            return Ok(Rect::default());
        }
        let mut ancestor = self.document.get_node(node).unwrap().parent;
        while let Some(parent) = ancestor {
            let parent_node = self.document.get_node(parent).unwrap();
            if parent_node
                .primary_styles()
                .is_some_and(|style| style.clone_display().is_none())
            {
                return Ok(Rect::default());
            }
            ancestor = parent_node.parent;
        }
        let Some(rect) = self.document.get_client_bounding_rect(node) else {
            return Ok(Rect {
                x: 0.0,
                y: 0.0,
                width: 0.0,
                height: 0.0,
                client_width: 0.0,
                client_height: 0.0,
                content_width: 0.0,
                content_height: 0.0,
                flags: 0,
            });
        };
        let flags = u32::from(self.document.is_in_skipped_content(node));
        let node = self.document.get_node(node).unwrap();
        Ok(Rect {
            x: rect.x as f32,
            y: rect.y as f32,
            width: rect.width as f32,
            height: rect.height as f32,
            client_width: node.client_width(),
            client_height: node.client_height(),
            content_width: node.scroll_width(),
            content_height: node.scroll_height(),
            flags,
        })
    }

    pub fn style(&self, id: u64, property: &str) -> Result<String, Error> {
        if self.dirty {
            return Err(Error::DirtyRead);
        }
        Ok(self.document.resolved_style_value(self.node(id)?, property))
    }

    pub fn style_batch(&self, id: u64, properties: &[&str]) -> Result<Vec<String>, Error> {
        if self.dirty {
            return Err(Error::DirtyRead);
        }
        Ok(self
            .document
            .resolved_style_values(self.node(id)?, properties))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture() -> Owner {
        let mut owner = Owner::new(1, 1280, 800);
        for (id, name) in [(2, "html"), (3, "body"), (4, "div")] {
            owner
                .element(id, "http://www.w3.org/1999/xhtml", name)
                .unwrap();
            owner.append(id - 1, id).unwrap();
        }
        owner
            .attribute(4, "", "style", "width:800px;height:32px")
            .unwrap();
        owner
    }

    #[test]
    fn persistent_mutation_and_clean_reads() {
        let mut owner = fixture();
        assert_eq!(owner.rect(4).unwrap_err(), Error::DirtyRead);
        assert!(owner.resolve(0.0));
        assert_eq!(owner.rect(4).unwrap().width, 800.0);
        assert!(!owner.resolve(0.0));
        assert_eq!(owner.generation, 1);
        owner
            .attribute(4, "", "style", "width:640px;height:40px")
            .unwrap();
        assert!(owner.resolve(0.0));
        assert_eq!(owner.rect(4).unwrap().width, 640.0);
        assert_eq!(owner.rect(4).unwrap().height, 40.0);
        assert_eq!(owner.generation, 2);
    }

    #[test]
    fn independent_documents() {
        let threads: Vec<_> = (0..4)
            .map(|_| {
                std::thread::spawn(|| {
                    let mut owner = fixture();
                    owner.resolve(0.0);
                    assert_eq!(owner.rect(4).unwrap().width, 800.0);
                })
            })
            .collect();
        for thread in threads {
            thread.join().unwrap();
        }
    }

    #[test]
    fn state_change_followed_by_stylesheet_invalidation_keeps_snapshot_attributes() {
        let mut owner = fixture();
        owner.attribute(4, "", "style", "height:32px").unwrap();
        owner.attribute(4, "", "class", "target").unwrap();
        owner.stylesheet(4, ".target:focus { color: red }").unwrap();
        assert!(owner.resolve(0.0));
        owner.state(4, 1 << 1, 1 << 1).unwrap();
        owner.stylesheet(4, ".target { width: 400px }").unwrap();
        assert!(owner.resolve(0.0));
        assert_eq!(owner.rect(4).unwrap().width, 400.0);
    }
}
