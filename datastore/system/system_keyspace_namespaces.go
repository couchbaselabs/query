//  Copyright (c) 2013 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package system

import (
	"fmt"

	"github.com/couchbaselabs/query/datastore"
	"github.com/couchbaselabs/query/errors"
	"github.com/couchbaselabs/query/expression"
	"github.com/couchbaselabs/query/timestamp"
	"github.com/couchbaselabs/query/value"
)

type namespaceKeyspace struct {
	namespace *namespace
	name      string
	indexer   datastore.Indexer
}

func (b *namespaceKeyspace) Release() {
}

func (b *namespaceKeyspace) NamespaceId() string {
	return b.namespace.Id()
}

func (b *namespaceKeyspace) Id() string {
	return b.Name()
}

func (b *namespaceKeyspace) Name() string {
	return b.name
}

func (b *namespaceKeyspace) Count() (int64, errors.Error) {
	namespaceIds, excp := b.namespace.store.actualStore.NamespaceIds()
	if excp == nil {
		return int64(len(namespaceIds)), nil
	}
	return 0, errors.NewSystemDatastoreError(excp, "")
}

func (b *namespaceKeyspace) Indexer(name datastore.IndexType) (datastore.Indexer, errors.Error) {
	return b.indexer, nil
}

func (b *namespaceKeyspace) Indexers() ([]datastore.Indexer, errors.Error) {
	return []datastore.Indexer{b.indexer}, nil
}

func (b *namespaceKeyspace) Fetch(keys []string) ([]datastore.AnnotatedPair, errors.Error) {
	rv := make([]datastore.AnnotatedPair, len(keys))
	for i, k := range keys {
		item, e := b.fetchOne(k)
		if e != nil {
			return nil, e
		}

		rv[i].Key = k
		rv[i].Value = item
	}
	return rv, nil
}

func (b *namespaceKeyspace) fetchOne(key string) (value.AnnotatedValue, errors.Error) {
	namespace, excp := b.namespace.store.actualStore.NamespaceById(key)
	if namespace != nil {
		doc := value.NewAnnotatedValue(map[string]interface{}{
			"id":           namespace.Id(),
			"name":         namespace.Name(),
			"datastore_id": b.namespace.store.actualStore.Id(),
		})
		return doc, nil
	}
	return nil, errors.NewSystemDatastoreError(excp, "Key Not Found "+key)
}

func (b *namespaceKeyspace) Insert(inserts []datastore.Pair) ([]datastore.Pair, errors.Error) {
	// FIXME
	return nil, errors.NewSystemNotImplementedError(nil, "")
}

func (b *namespaceKeyspace) Update(updates []datastore.Pair) ([]datastore.Pair, errors.Error) {
	// FIXME
	return nil, errors.NewSystemNotImplementedError(nil, "")
}

func (b *namespaceKeyspace) Upsert(upserts []datastore.Pair) ([]datastore.Pair, errors.Error) {
	// FIXME
	return nil, errors.NewSystemNotImplementedError(nil, "")
}

func (b *namespaceKeyspace) Delete(deletes []string) ([]string, errors.Error) {
	// FIXME
	return nil, errors.NewSystemNotImplementedError(nil, "")
}

func newNamespacesKeyspace(p *namespace) (*namespaceKeyspace, errors.Error) {
	b := new(namespaceKeyspace)
	b.namespace = p
	b.name = KEYSPACE_NAME_NAMESPACES

	primary := &namespaceIndex{name: "#primary", keyspace: b}
	b.indexer = &systemIndexer{keyspace: b, indexes: make(map[string]datastore.Index), primary: primary}

	return b, nil
}

type namespaceIndex struct {
	name     string
	keyspace *namespaceKeyspace
}

func (pi *namespaceIndex) KeyspaceId() string {
	return pi.keyspace.Id()
}

func (pi *namespaceIndex) Id() string {
	return pi.Name()
}

func (pi *namespaceIndex) Name() string {
	return pi.name
}

func (pi *namespaceIndex) Type() datastore.IndexType {
	return datastore.DEFAULT
}

func (pi *namespaceIndex) SeekKey() expression.Expressions {
	return nil
}

func (pi *namespaceIndex) RangeKey() expression.Expressions {
	return nil
}

func (pi *namespaceIndex) Condition() expression.Expression {
	return nil
}

func (pi *namespaceIndex) State() (state datastore.IndexState, msg string, err errors.Error) {
	return datastore.ONLINE, "", nil
}

func (pi *namespaceIndex) Statistics(span *datastore.Span) (datastore.Statistics, errors.Error) {
	return nil, nil
}

func (pi *namespaceIndex) Drop() errors.Error {
	return errors.NewSystemIdxNoDropError(nil, "")
}

func (pi *namespaceIndex) Scan(span *datastore.Span, distinct bool, limit int64,
	cons datastore.ScanConsistency, vector timestamp.Vector, conn *datastore.IndexConnection) {
	defer close(conn.EntryChannel())

	val := ""

	a := span.Seek[0].Actual()
	switch a := a.(type) {
	case string:
		val = a
	default:
		conn.Error(errors.NewSystemDatastoreError(nil, fmt.Sprintf("Invalid seek value %v of type %T.", a, a)))
		return
	}

	namespace, _ := pi.keyspace.namespace.store.actualStore.NamespaceById(val)
	if namespace != nil {
		entry := datastore.IndexEntry{PrimaryKey: namespace.Id()}
		conn.EntryChannel() <- &entry
	}
}

func (pi *namespaceIndex) ScanEntries(limit int64, cons datastore.ScanConsistency,
	vector timestamp.Vector, conn *datastore.IndexConnection) {
	defer close(conn.EntryChannel())

	namespaceIds, err := pi.keyspace.namespace.store.actualStore.NamespaceIds()
	if err == nil {
		for i, namespaceId := range namespaceIds {
			if limit > 0 && int64(i) > limit {
				break
			}

			entry := datastore.IndexEntry{PrimaryKey: namespaceId}
			conn.EntryChannel() <- &entry
		}
	}
}
