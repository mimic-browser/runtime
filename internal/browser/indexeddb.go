package browser

import (
	"context"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// IndexedDB owns immutable storage snapshots at Context/origin scope. A queue
// serializes transactions, including upgrades, across independent Page loops.
// No V8 object, callback, or isolate-owned value is retained in stored records.
type indexedDatabase struct {
	name        string
	version     uint64
	data        string
	connections map[uint64]*indexedConnection
	queue       []*indexedJob
	active      *indexedJob
}
type indexedConnection struct {
	id    uint64
	realm *Realm
	token uint64
}
type indexedJob struct {
	realm                      *Realm
	token, connection, version uint64
	kind                       string
	notified                   bool
}

func (r *Realm) indexedNotify(token uint64, kind string, row map[string]any) {
	r.scheduler.Post(scheduler.Source("database"), 0, func(ctx context.Context) error {
		if r.indexedNotifier == nil {
			return nil
		}
		if row == nil {
			row = map[string]any{}
		}
		row["kind"] = kind
		row["token"] = token
		_, err := r.runtime.Call(ctx, r.indexedNotifier, nil, r.val(row))
		return err
	})
}
func (c *Context) pumpIndexed(db *indexedDatabase) {
	if db.active != nil || len(db.queue) == 0 {
		return
	}
	j := db.queue[0]
	if j.kind == "open" && j.version != 0 && j.version < db.version {
		db.queue = db.queue[1:]
		j.realm.indexedNotify(j.token, "error", map[string]any{"error": "VersionError"})
		c.pumpIndexed(db)
		return
	}
	version := j.version
	if version == 0 {
		version = db.version
		if version == 0 {
			version = 1
		}
	}
	exclusive := j.kind == "delete" || (j.kind == "open" && version > db.version)
	if exclusive && len(db.connections) > 0 {
		if !j.notified {
			j.notified = true
			var next any = version
			if j.kind == "delete" {
				next = nil
			}
			for _, conn := range db.connections {
				conn.realm.indexedNotify(conn.token, "versionchange", map[string]any{"oldVersion": db.version, "newVersion": next})
			}
			// Blocked is checked in a later database task: close() in versionchange
			// handlers must get its opportunity before the request is called blocked.
			j.realm.scheduler.Post(scheduler.Source("database"), 0, func(ctx context.Context) error {
				c.mu.Lock()
				defer c.mu.Unlock()
				if len(db.queue) > 0 && db.queue[0] == j && len(db.connections) > 0 {
					j.realm.indexedNotify(j.token, "blocked", map[string]any{"oldVersion": db.version, "newVersion": next})
				}
				return nil
			})
		}
		return
	}
	db.queue = db.queue[1:]
	if j.kind == "delete" {
		old := db.version
		db.version = 0
		db.data = ""
		j.realm.indexedNotify(j.token, "deleted", map[string]any{"oldVersion": old})
		c.pumpIndexed(db)
		return
	}
	if j.kind == "open" {
		c.indexedSequence++
		conn := &indexedConnection{id: c.indexedSequence, realm: j.realm, token: j.token}
		db.connections[conn.id] = conn
		j.connection = conn.id
		row := map[string]any{"connection": conn.id, "name": db.name, "version": version, "oldVersion": db.version, "data": db.data}
		if version > db.version {
			db.active = j
			j.version = version
			j.realm.indexedNotify(j.token, "upgrade", row)
		} else {
			j.realm.indexedNotify(j.token, "opened", row)
			c.pumpIndexed(db)
		}
		return
	}
	if db.connections[j.connection] == nil {
		j.realm.indexedNotify(j.token, "error", map[string]any{"error": "InvalidStateError"})
		c.pumpIndexed(db)
		return
	}
	db.active = j
	j.realm.indexedNotify(j.token, "transaction", map[string]any{"data": db.data})
}
func addIndexedDBHosts(r *Realm, h map[string]any) {
	h["indexedDBTaskIdentity"] = r.fn(func(_ engine.Value, _ []engine.Value) (engine.Value, error) {
		p := r.agent.Page()
		// A nested Page task can leave its caller's scheduler marked running.
		// The Page clock tracks the innermost task; map iteration cannot select
		// the task whose transaction activity is currently being observed.
		if current := p.activeClock.Load(); current != nil {
			status := current.ExecutionStatus()
			if status.Running {
				return r.val("task:" + strconv.FormatUint(status.TaskID, 10)), nil
			}
		}
		return r.val("script:" + strconv.FormatUint(p.databaseScriptEpoch, 10)), nil
	})
	h["installIndexedDBEncoder"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) { r.indexedEncoder = a[0]; return nil, nil })
	h["indexedDBCloneReference"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		owner, err := r.referenceRealm(strarg(a, 0), strarg(a, 2))
		if err != nil {
			return nil, err
		}
		return r.crossFrameData(owner, func(ctx context.Context) (any, error) {
			value := owner.crossValues[int64(numarg(a, 1))]
			if value == nil || owner.indexedEncoder == nil {
				return []any{false, "DataCloneError", "The source realm is unavailable"}, nil
			}
			encoded, err := owner.runtime.Call(ctx, owner.indexedEncoder, nil, value)
			if err != nil {
				return nil, err
			}
			return encoded.Export(), nil
		})
	})
	h["indexedDBValidKeyPath"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		path := strarg(a, 0)
		if path == "" {
			return r.val(true), nil
		}
		valid := true
		for _, part := range strings.Split(path, ".") {
			if part == "" {
				valid = false
				break
			}
			for i, ch := range part {
				start := ch == '$' || ch == '_' || unicode.IsLetter(ch) || unicode.Is(unicode.Nl, ch) || unicode.Is(unicode.Other_ID_Start, ch)
				more := unicode.IsDigit(ch) || unicode.Is(unicode.Mn, ch) || unicode.Is(unicode.Mc, ch) || unicode.Is(unicode.Pc, ch) || unicode.Is(unicode.Other_ID_Continue, ch) || ch == 0x200c || ch == 0x200d
				if !start && (i == 0 || !more) {
					valid = false
					break
				}
			}
		}
		return r.val(valid), nil
	})
	h["installIndexedDBNotifier"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.indexedNotifier = a[0]
		return nil, nil
	})
	h["indexedDBTask"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		fn := a[0]
		r.scheduler.Post(scheduler.Source("database"), 0, func(ctx context.Context) error { _, err := r.runtime.Call(ctx, fn, nil); return err })
		return nil, nil
	})
	h["indexedDB"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		c := r.agent.Page().ctx
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.indexedDatabases == nil {
			c.indexedDatabases = map[string]map[string]*indexedDatabase{}
		}
		catalog := c.indexedDatabases[r.origin]
		if catalog == nil {
			catalog = map[string]*indexedDatabase{}
			c.indexedDatabases[r.origin] = catalog
		}
		op, name := strarg(a, 0), strarg(a, 1)
		if op == "databases" {
			rows := []map[string]any{}
			names := []string{}
			for n, db := range catalog {
				if db.version > 0 {
					names = append(names, n)
				}
			}
			sort.Strings(names)
			for _, n := range names {
				rows = append(rows, map[string]any{"name": n, "version": catalog[n].version})
			}
			return r.val(rows), nil
		}
		db := catalog[name]
		if db == nil {
			db = &indexedDatabase{name: name, connections: map[uint64]*indexedConnection{}}
			catalog[name] = db
		}
		token := uint64(numarg(a, 2))
		switch op {
		case "open", "delete":
			db.queue = append(db.queue, &indexedJob{realm: r, kind: op, token: token, version: uint64(numarg(a, 3))})
			c.pumpIndexed(db)
		case "begin":
			db.queue = append(db.queue, &indexedJob{realm: r, kind: op, token: token, connection: uint64(numarg(a, 3))})
			c.pumpIndexed(db)
		case "finish":
			// Aborting before grant removes the pending job without acquiring a lock.
			remaining := db.queue[:0]
			for _, queued := range db.queue {
				if queued.realm != r || queued.token != token {
					remaining = append(remaining, queued)
				}
			}
			db.queue = remaining
			if j := db.active; j != nil && j.realm == r && j.token == token {
				commit, _ := arg(a, 3).(bool)
				if commit {
					db.data = strarg(a, 4)
					if j.kind == "open" {
						db.version = j.version
					}
				}
				if j.kind == "open" && !commit {
					delete(db.connections, j.connection)
				}
			}
		case "release":
			// Completion/success is delivered before a queued connection or
			// transaction can observe the committed version.
			if j := db.active; j != nil && j.realm == r && j.token == token {
				db.active = nil
				c.pumpIndexed(db)
			}
		case "close":
			if conn := db.connections[token]; conn != nil && conn.realm == r {
				delete(db.connections, token)
				c.pumpIndexed(db)
			}
		}
		return nil, nil
	})
}
func (r *Realm) closeIndexedDatabases() {
	c := r.agent.Page().ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, catalog := range c.indexedDatabases {
		for _, db := range catalog {
			for id, conn := range db.connections {
				if conn.realm == r {
					delete(db.connections, id)
				}
			}
			q := db.queue[:0]
			for _, j := range db.queue {
				if j.realm != r {
					q = append(q, j)
				}
			}
			db.queue = q
			if db.active != nil && db.active.realm == r {
				db.active = nil
			}
			c.pumpIndexed(db)
		}
	}
}
