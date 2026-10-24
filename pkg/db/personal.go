package db

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dgraph-io/badger"
)

// PersonalTx keeps media references, memberships and the initialization marker
// in one Badger transaction, independently of channel metadata.
type PersonalTx interface {
	Get(key string, out interface{}) error
	Put(key string, value interface{}) error
	Delete(key string) error
	Walk(prefix string, fn func(key string, value []byte) error) error
}

type PersonalStorage interface {
	PersonalView(func(PersonalTx) error) error
	PersonalUpdate(func(PersonalTx) error) error
}

type personalTx struct {
	b   *Badger
	txn *badger.Txn
}

const personalPrefix = "podsync/personal/v1/"

func (t personalTx) Get(key string, out interface{}) error {
	return t.b.getObj(t.txn, []byte(personalPrefix+key), out)
}
func (t personalTx) Put(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return t.txn.Set([]byte(personalPrefix+key), data)
}
func (t personalTx) Delete(key string) error { return t.txn.Delete([]byte(personalPrefix + key)) }
func (t personalTx) Walk(prefix string, fn func(string, []byte) error) error {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = []byte(personalPrefix + prefix)
	return t.b.iterator(context.Background(), t.txn, opts, func(item *badger.Item) error {
		data, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		return fn(strings.TrimPrefix(string(item.Key()), personalPrefix), data)
	})
}
func (b *Badger) PersonalView(fn func(PersonalTx) error) error {
	return b.db.View(func(txn *badger.Txn) error { return fn(personalTx{b, txn}) })
}
func (b *Badger) PersonalUpdate(fn func(PersonalTx) error) error {
	// A second worker may complete another download at the same time.
	for attempt := 0; attempt < 10; attempt++ {
		err := b.db.Update(func(txn *badger.Txn) error { return fn(personalTx{b, txn}) })
		if err != badger.ErrConflict {
			return err
		}
	}
	return badger.ErrConflict
}

var _ PersonalStorage = (*Badger)(nil)
