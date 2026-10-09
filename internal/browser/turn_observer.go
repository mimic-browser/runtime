package browser

// TurnSubscription observes completed Page turns under the command boundary.
// Consumers may read canonical state or enqueue a coalesced readiness hint;
// they must not acquire the boundary again. No timer or journal is introduced.
type TurnSubscription struct{ publish func() }

func (p *Page) SubscribeTurn(publish func()) (*TurnSubscription, func()) {
	sub := &TurnSubscription{publish: publish}
	if p.turnObservers == nil {
		p.turnObservers = make(map[*TurnSubscription]struct{})
	}
	p.turnObservers[sub] = struct{}{}
	return sub, func() {
		delete(p.turnObservers, sub)
		if len(p.turnObservers) == 0 {
			p.turnObservers = nil
		}
	}
}

func (p *Page) publishTurn() {
	for sub := range p.turnObservers {
		sub.publish()
	}
}
