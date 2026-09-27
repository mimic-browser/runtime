if (globalThis.HTMLDialogElement) {
  const toggleSlots = new WeakMap();
  class ToggleEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      const source = init.source ?? null;
      if (source !== null && !(source instanceof Element))
        throw new TypeError('source must be an Element');
      toggleSlots.set(this, {
        oldState: String(init.oldState ?? ''),
        newState: String(init.newState ?? ''),
        source,
      });
    }
  }
  for (const key of ['oldState', 'newState', 'source'])
    Object.defineProperty(ToggleEvent.prototype, key, {
      get() {
        const state = toggleSlots.get(this);
        if (!state) throw new TypeError('Illegal invocation');
        return state[key];
      },
      enumerable: true,
      configurable: true,
    });
  Object.defineProperty(ToggleEvent.prototype, Symbol.toStringTag, {
    value: 'ToggleEvent',
    configurable: true,
  });
  markNative(ToggleEvent, 'ToggleEvent');
  Object.defineProperty(globalThis, 'ToggleEvent', {
    value: ToggleEvent,
    writable: true,
    configurable: true,
  });
  const pending = new WeakMap(),
    previousFocus = new WeakMap(),
    closing = new WeakSet();
  const check = (node) => {
    if (elementSlot(node)?.tagName !== 'DIALOG') throw new TypeError('Illegal invocation');
    return node;
  };
  const queueToggle = (node, oldState, newState, source) => {
    const previous = pending.get(node);
    if (previous) {
      clearTimeout(previous.timer);
      oldState = previous.oldState;
    }
    const timer = setTimeout(() => {
      pending.delete(node);
      dispatchTrusted(node, new ToggleEvent('toggle', { oldState, newState, source }));
    }, 0);
    pending.set(node, { timer, oldState });
  };
  const before = (node, opening, source) =>
    dispatchTrusted(
      node,
      new ToggleEvent('beforetoggle', {
        oldState: opening ? 'closed' : 'open',
        newState: opening ? 'open' : 'closed',
        cancelable: opening,
        source,
      }),
    );
  const focusDialog = (node) => {
    previousFocus.set(node, compatibilityElementState.focused());
    const eligible = (el) =>
      !el.disabled &&
      !el.closest('[inert]') &&
      getComputedStyle(el).display !== 'none' &&
      getComputedStyle(el).visibility !== 'hidden';
    const candidates = Array.from(
      node.querySelectorAll('[autofocus],button,input,select,textarea,a[href],[tabindex]'),
    );
    const target = node.hasAttribute('autofocus')
      ? node
      : candidates.find((el) => el.hasAttribute('autofocus') && eligible(el)) ||
        candidates.find(
          (el) => (el.localName !== 'input' || el.type !== 'hidden') && eligible(el),
        ) ||
        node;
    target.focus();
  };
  const show = (node, modal, source = null) => {
    check(node);
    if (node.open) {
      if (modal !== modalDialogs.has(node))
        throw platformDOMException('Dialog is already open in another mode', 'InvalidStateError');
      return;
    }
    if (modal && !node.isConnected)
      throw platformDOMException('Dialog is not connected', 'InvalidStateError');
    if (!before(node, true, source) || node.open || (modal && !node.isConnected)) return;
    node.open = true;
    if (modal) modalDialogs.add(node);
    queueToggle(node, 'closed', 'open', source);
    focusDialog(node);
  };
  const close = (node, value, source = null) => {
    check(node);
    if (value !== undefined) value = bindingString(value);
    if (!node.open || closing.has(node)) return;
    closing.add(node);
    try {
      before(node, false, source);
      if (!node.open) return;
      const modal = modalDialogs.has(node),
        previous = previousFocus.get(node),
        inside = node.contains(compatibilityElementState.focused());
      queueToggle(node, 'open', 'closed', source);
      node.open = false;
      modalDialogs.delete(node);
      if (value !== undefined) dialogReturnValues.set(node, value);
      if (previous?.isConnected && (modal || inside)) previous.focus();
      previousFocus.delete(node);
      setTimeout(() => dispatchTrusted(node, new Event('close')), 0);
    } finally {
      closing.delete(node);
    }
  };
  compatibilityElementState.showDialog = (node, source) => show(node, true, source);
  compatibilityElementState.closeDialog = close;
  compatibilityElementState.dialogAttributeChanged = (node, name, oldValue) => {
    if (
      name === 'open' &&
      node.localName === 'dialog' &&
      oldValue !== null &&
      !node.open &&
      node.contains(focused)
    ) {
      // Rendering a hidden dialog clears descendant focus, but removing open
      // alone neither closes the top layer nor fires dialog lifecycle events.
      setTimeout(() => {
        if (!node.open && node.contains(focused)) focused = null;
      }, 0);
    }
  };
  Object.defineProperties(HTMLDialogElement.prototype, {
    open: {
      get() {
        return check(this).hasAttribute('open');
      },
      set(value) {
        check(this);
        if (value) this.setAttribute('open', '');
        else this.removeAttribute('open');
      },
      configurable: true,
      enumerable: true,
    },
    returnValue: {
      get() {
        check(this);
        return dialogReturnValues.get(this) || '';
      },
      set(value) {
        check(this);
        dialogReturnValues.set(this, bindingString(value));
      },
      configurable: true,
      enumerable: true,
    },
    show: {
      value: {
        show() {
          show(this, false);
        },
      }.show,
      writable: true,
      configurable: true,
      enumerable: true,
    },
    showModal: {
      value: {
        showModal() {
          show(this, true);
        },
      }.showModal,
      writable: true,
      configurable: true,
      enumerable: true,
    },
    close: {
      value: {
        close(value) {
          close(this, value);
        },
      }.close,
      writable: true,
      configurable: true,
      enumerable: true,
    },
    requestClose: {
      value: {
        requestClose(value) {
          check(this);
          if (value !== undefined) value = bindingString(value);
          if (this.open && dispatchTrusted(this, new Event('cancel', { cancelable: true })))
            close(this, value);
        },
      }.requestClose,
      writable: true,
      configurable: true,
      enumerable: true,
    },
  });
}
