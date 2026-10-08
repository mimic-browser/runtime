package browser

import "errors"

// Contexts returns a public registry snapshot; constructing or executing a Page never
// holds the browser registry lock. Each Context retains its own cookie/storage
// and transport ownership.
func (b *Browser) Contexts() []*Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]*Context, 0, len(b.contexts))
	for _, c := range b.contexts {
		if c.privateWorkDone == nil {
			out = append(out, c)
		}
	}
	return out
}

func (b *Browser) Context(id string) (*Context, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	c, ok := b.contexts[id]
	if ok && c.privateWorkDone != nil {
		return nil, false
	}
	return c, ok
}

func (b *Browser) CloseContext(id string) error {
	b.mu.Lock()
	c := b.contexts[id]
	if c != nil && c.privateWorkDone != nil {
		b.mu.Unlock()
		return nil
	}
	delete(b.contexts, id)
	b.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Close()
}

// Close releases every Context before disposing the immutable bootstrap
// artifacts shared by this Browser. It is safe to call more than once.
func (b *Browser) Close() error {
	b.mu.Lock()
	if b.closed {
		done := b.closeDone
		b.mu.Unlock()
		if done != nil {
			<-done
		}
		b.mu.RLock()
		err := b.closeErr
		b.mu.RUnlock()
		return err
	}
	b.closed = true
	b.closeDone = make(chan struct{})
	b.cancel()
	contexts := make([]*Context, 0, len(b.contexts))
	for _, c := range b.contexts {
		contexts = append(contexts, c)
	}
	b.contexts = map[string]*Context{}
	b.mu.Unlock()

	var result error
	for _, c := range contexts {
		result = errors.Join(result, c.Close())
	}
	result = errors.Join(result, b.bootstrapSnapshots.close())

	b.mu.Lock()
	b.closeErr = result
	close(b.closeDone)
	b.mu.Unlock()
	return result
}

func (p *Page) ContextID() string { return p.ctx.ID }
