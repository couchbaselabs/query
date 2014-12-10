//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

/*

Package file provides a couchbase-server implementation of the datasite
package.

*/

package couchbase

import (
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/couchbase/cbauth"
	cb "github.com/couchbaselabs/go-couchbase"
	"github.com/couchbaselabs/query/datastore"
	"github.com/couchbaselabs/query/errors"
	"github.com/couchbaselabs/query/expression"
	"github.com/couchbaselabs/query/logging"
	"github.com/couchbaselabs/query/value"
)

const (
	PRIMARY_INDEX = "#primary"
	ALLDOCS_INDEX = "#alldocs"
)

// datasite is the root for the couchbase datasite
type site struct {
	client         cb.Client             // instance of go-couchbase client
	namespaceCache map[string]*namespace // map of pool-names and IDs
}

// Admin credentials
type credentials struct {
	user     string // username,i.e. Administrator
	password string //  Administrator password
}

func (s *site) Id() string {
	return s.URL()
}

func (s *site) URL() string {
	return s.client.BaseURL.String()
}

func (s *site) NamespaceIds() ([]string, errors.Error) {
	return s.NamespaceNames()
}

func (s *site) NamespaceNames() ([]string, errors.Error) {
	return []string{"default"}, nil
}

func (s *site) NamespaceById(id string) (p datastore.Namespace, e errors.Error) {
	return s.NamespaceByName(id)
}

func (s *site) NamespaceByName(name string) (p datastore.Namespace, e errors.Error) {
	p, ok := s.namespaceCache[name]
	if !ok {
		var err errors.Error
		p, err = loadNamespace(s, name)
		if err != nil {
			return nil, err
		}
		s.namespaceCache[name] = p.(*namespace)
	}
	return p, nil
}

// NewSite creates a new Couchbase site for the given url.
func NewDatastore(u string) (s datastore.Datastore, e errors.Error) {

	up, err := url.Parse(u)
	if err != nil {
		return nil, errors.NewError(nil, fmt.Sprintf("Failed to parse url %s", u))
	}
	hostport := up.Host

	if hostport == "" {
		return nil, errors.NewError(nil, fmt.Sprintf("Invalid url %s", u))
	}

	logging.Infof("Connecting to %s", hostport)
	httpUser, httpPassword, err := cbauth.GetHTTPServiceAuth(hostport)
	if err != nil {
		return nil, errors.NewError(err, "")
	}

	client, err := cb.ConnectWithAuthCreds(u, httpUser, httpPassword)
	if err != nil {
		return nil, errors.NewError(err, "Cannot connect to url "+u)
	}

	site := &site{
		client:         client,
		namespaceCache: make(map[string]*namespace),
	}

	// initialize the default pool.
	// TODO can couchbase server contain more than one pool ?

	defaultPool, Err := loadNamespace(site, "default")
	if Err != nil {
		logging.Errorf("Cannot connect to default pool")
		return nil, Err
	}

	site.namespaceCache["default"] = defaultPool
	logging.Infof("New site created with url %s", u)

	return site, nil
}

func loadNamespace(s *site, name string) (*namespace, errors.Error) {

	cbpool, err := s.client.GetPool(name)
	if err != nil {
		if name == "default" {

			u := s.URL()
			up, _ := url.Parse(u)
			hostport := up.Host
			// get http service user-name & password
			httpUser, httpPassword, err := cbauth.GetHTTPServiceAuth(hostport)
			if err != nil {
				return nil, errors.NewError(err, "")
			}

			// if default pool is not available, try reconnecting to the server
			client, err := cb.ConnectWithAuthCreds(u, httpUser, httpPassword)
			if err != nil {
				return nil, errors.NewError(nil, fmt.Sprintf("Pool %v not found.", name))
			}
			// check if the default pool exists
			cbpool, err = client.GetPool(name)
			if err != nil {
				return nil, errors.NewError(nil, fmt.Sprintf("Pool %v not found.", name))
			}
			s.client = client
		}
	}

	rv := namespace{
		site:          s,
		name:          name,
		cbNamespace:   cbpool,
		keyspaceCache: make(map[string]datastore.Keyspace),
	}
	go keepPoolFresh(&rv)
	return &rv, nil
}

// a namespace represents a couchbase pool
type namespace struct {
	site             *site
	name             string
	cbNamespace      cb.Pool
	keyspaceCache    map[string]datastore.Keyspace
	lock             sync.Mutex   // lock to guard the keyspaceCache
	nslock           sync.RWMutex // lock for this structure
	adminCredentials *credentials
}

func (p *namespace) DatastoreId() string {
	return p.site.Id()
}

func (p *namespace) Id() string {
	return p.Name()
}

func (p *namespace) Name() string {
	return p.name
}

func (p *namespace) KeyspaceIds() ([]string, errors.Error) {
	return p.KeyspaceNames()
}

func (p *namespace) KeyspaceNames() ([]string, errors.Error) {
	rv := make([]string, 0, len(p.cbNamespace.BucketMap))
	for name, _ := range p.cbNamespace.BucketMap {
		rv = append(rv, name)
	}
	return rv, nil
}

func (p *namespace) KeyspaceByName(name string) (b datastore.Keyspace, e errors.Error) {

	b, ok := p.keyspaceCache[name]
	if !ok {
		var err errors.Error
		b, err = newKeyspace(p, name)
		if err != nil {
			return nil, errors.NewError(err, "Keyspace "+name+" name not found")
		}
		p.lock.Lock()
		defer p.lock.Unlock()
		p.keyspaceCache[name] = b
	}
	return b, nil
}

func (p *namespace) KeyspaceById(id string) (datastore.Keyspace, errors.Error) {
	return p.KeyspaceByName(id)
}

func (p *namespace) setPool(cbpool cb.Pool) {
	p.nslock.Lock()
	defer p.nslock.Unlock()
	p.cbNamespace = cbpool
}

func (p *namespace) getPool() cb.Pool {
	p.nslock.RLock()
	defer p.nslock.RUnlock()
	return p.cbNamespace
}

func (p *namespace) refresh(changed bool) {

	var hostport string
	// trigger refresh of this pool
	logging.Infof("Refreshing pool %s", p.name)
	u := p.site.URL()
	up, _ := url.Parse(u)
	hostport = up.Host

	newpool, err := p.site.client.GetPool(p.name)
	if err != nil {
		logging.Errorf("Error updating pool name %s: Error %v", p.name, err)

		httpUser, httpPassword, err := cbauth.GetHTTPServiceAuth(hostport)
		if err != nil {
			logging.Errorf("Failed to get Service credentials %v", err)
			return
		}

		client, err := cb.ConnectWithAuthCreds(u, httpUser, httpPassword)
		if err != nil {
			logging.Errorf("Error connecting to URL %s", u)
			return
		}
		// check if the default pool exists
		newpool, err = client.GetPool(p.name)
		if err != nil {
			logging.Errorf("Retry Failed Error updating pool name %s: Error %v", p.name, err)
			return
		}
		p.site.client = client

	}

	mUser, mPassword, err := cbauth.GetMemcachedServiceAuth(hostport)
	if err != nil {
		logging.Errorf("Failed to get Memcached Service credentials %v", err)
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()
	for name, keySpace := range p.keyspaceCache {
		logging.Infof(" Checking keyspace %s", name)
		_, err := newpool.GetBucketWithAuth(name, mUser, mPassword)
		if err != nil {
			changed = true
			keySpace.(*keyspace).deleted = true
			logging.Errorf(" Error retrieving bucket %s", name)
			delete(p.keyspaceCache, name)

		}
	}

	if changed == true {
		p.setPool(newpool)
	}
}

func keepPoolFresh(p *namespace) {

	tickChan := time.Tick(15 * time.Second)

	for _ = range tickChan {
		p.refresh(false)
	}
}

type keyspace struct {
	namespace        *namespace
	name             string
	indexes          map[string]datastore.Index
	primary          datastore.PrimaryIndex
	cbbucket         *cb.Bucket
	deleted          bool
	nonUsableIndexes []string // indexes that cannot be used
	saslPassword     string   // SASL password
}

func (b *keyspace) refresh() {
	// trigger refresh of this pool
	logging.Infof("Refreshing Indexes in keyspace %s", b.name)

	indexes, err := loadViewIndexes(b)
	if err != nil {
		logging.Errorf(" Error loading indexes for bucket %s", b.name)
		return
	}

	if len(indexes) == 0 {
		return
	}

	for _, index := range indexes {
		logging.Infof("Found index %s  on keyspace %s", (*index).Name(), b.name)
		name := (*index).Name()
		b.indexes[name] = *index
	}

}

func keepIndexesFresh(b *keyspace) {

	tickChan := time.Tick(15 * time.Second)

	for _ = range tickChan {
		if b.deleted == true {
			return
		}
		b.refresh()
	}
}

func newKeyspace(p *namespace, name string) (datastore.Keyspace, errors.Error) {

	var saslPassword string
	var cbbucket *cb.Bucket

	cbNamespace := p.getPool()

	u := p.site.URL()
	up, _ := url.Parse(u)
	hostport := up.Host

	mUser, mPassword, err := cbauth.GetMemcachedServiceAuth(hostport)
	if err != nil {
		return nil, errors.NewError(nil, fmt.Sprintf("Failed to get Memcached Service credentials %v", err))
	}

	cbbucket, err = cbNamespace.GetBucketWithAuth(name, mUser, mPassword)

	if err != nil {
		logging.Infof(" keyspace %s not found %v", name, err)
		// go-couchbase caches the buckets
		// to be sure no such bucket exists right now
		// we trigger a refresh
		p.refresh(true)
		cbNamespace = p.getPool()

		// and then check one more time
		logging.Infof(" Retrying bucket %s", name)
		cbbucket, err = cbNamespace.GetBucketWithAuth(name, mUser, mPassword)
		if err != nil {
			// really no such bucket exists
			return nil, errors.NewError(err, fmt.Sprintf("Bucket %v not found.", name))
		}
	}

	rv := &keyspace{
		namespace:        p,
		name:             name,
		cbbucket:         cbbucket,
		indexes:          make(map[string]datastore.Index),
		nonUsableIndexes: make([]string, 0),
		saslPassword:     saslPassword,
	}

	logging.Infof("Created New Bucket %s", name)

	//discover existing indexes
	if ierr := rv.loadIndexes(); ierr != nil {
		logging.Warnf("Error loading indexes for keyspace %s, Error %v", name, ierr)
	}

	go keepIndexesFresh(rv)

	return rv, nil
}

func (b *keyspace) NamespaceId() string {
	return b.namespace.Id()
}

func (b *keyspace) Id() string {
	return b.Name()
}

func (b *keyspace) Name() string {
	return b.name
}

func (b *keyspace) Count() (int64, errors.Error) {
	var err error

	statsMap := b.cbbucket.GetStats("")
	for _, stats := range statsMap {
		itemCount := stats["curr_items_tot"]
		if totalCount, err := strconv.Atoi(itemCount); err == nil {
			return int64(totalCount), nil
		}

	}

	pi, err := b.IndexByPrimary()
	if err != nil || pi == nil {
		return 0, errors.NewError(nil, "Unable to get item count and no primary index found for bucket "+b.Name())
	}

	var totalCount int64

	switch pi := pi.(type) {
	case *primaryIndex:
		vi := pi
		totalCount, err = ViewTotalRows(vi.keyspace.cbbucket, vi.DDocName(), vi.ViewName(), map[string]interface{}{})
	case *viewIndex:
		vi := pi
		totalCount, err = ViewTotalRows(vi.keyspace.cbbucket, vi.DDocName(), vi.ViewName(), map[string]interface{}{})
	}

	if err != nil {
		return 0, errors.NewError(err, "")
	}

	return totalCount, nil
}

func (b *keyspace) Indexer(name datastore.IndexType) (datastore.Indexer, errors.Error) {
	return nil, errors.NewError(nil, "Not yet implemented.")
}

func (b *keyspace) Indexers() ([]datastore.Indexer, errors.Error) {
	return nil, errors.NewError(nil, "Not yet implemented.")
}

func (b *keyspace) IndexIds() ([]string, errors.Error) {
	rv := make([]string, 0, len(b.indexes))
	for name, _ := range b.indexes {
		rv = append(rv, name)
	}
	return rv, nil
}

func (b *keyspace) IndexNames() ([]string, errors.Error) {
	rv := make([]string, 0, len(b.indexes))
	for name, _ := range b.indexes {
		rv = append(rv, name)
	}
	return rv, nil
}

func (b *keyspace) IndexById(id string) (datastore.Index, errors.Error) {
	return b.IndexByName(id)
}

func (b *keyspace) IndexByName(name string) (datastore.Index, errors.Error) {
	index, ok := b.indexes[name]
	if !ok {
		return nil, errors.NewError(nil, fmt.Sprintf("Index %v not found.", name))
	}
	return index, nil
}

func (b *keyspace) IndexByPrimary() (datastore.PrimaryIndex, errors.Error) {

	if b.primary == nil {

		logging.Infof("Number of indexes %d", len(b.indexes))

		if len(b.indexes) == 0 {
			if err := b.loadIndexes(); err != nil {
				return nil, errors.NewError(err, "No indexes found. Please create a primary index")

			}

		}
		idx, ok := b.indexes[PRIMARY_INDEX]
		if ok {
			primary := idx.(datastore.PrimaryIndex)
			return primary, nil
		}
		all, ok := b.indexes[ALLDOCS_INDEX]
		if ok {
			primary := all.(datastore.PrimaryIndex)
			return primary, nil
		}
	}
	return b.primary, nil
}

func (b *keyspace) Indexes() ([]datastore.Index, errors.Error) {
	rv := make([]datastore.Index, 0, len(b.indexes))
	for _, index := range b.indexes {
		rv = append(rv, index)
	}
	return rv, nil
}

func doAuth(username, password, bucket string, requested datastore.Privileges) (bool, error) {

	logging.Infof(" Authenticating for bucket %s username %s password %s", bucket, username, password)
	creds, err := cbauth.Auth(username, password)
	if err != nil {
		return false, err
	}

	if (requested & datastore.CAN_DDL) != 0 {
		authResult, err := creds.CanDDLBucket(bucket)
		if err != nil || authResult == false {
			return false, err
		}

	} else if (requested & datastore.CAN_WRITE) != 0 {
		authResult, err := creds.CanAccessBucket(bucket)
		if err != nil || authResult == false {
			return false, err
		}

	} else if (requested & datastore.CAN_READ) != 0 {
		authResult, err := creds.CanReadBucket(bucket)
		if err != nil || authResult == false {
			return false, err
		}

	} else {
		return false, fmt.Errorf("Invalid Privileges")
	}

	return true, nil

}

func (b *keyspace) Authenticate(credentials datastore.Credentials, requested datastore.Privileges) errors.Error {

	var authResult bool
	var err error

	// No auth required for default bucket
	if b.Name() == "default" {
		return nil
	}

	logging.Infof("Authenticating for bucket %s", b.Name())

	for _, cred := range credentials {
		username := cred.Username()
		password, _ := cred.Password()

		userCreds := strings.Split(username, ":")

		if strings.EqualFold(userCreds[0], "admin") {
			authResult, err = doAuth(userCreds[1], password, b.Name(), requested)
		} else if len(userCreds) > 1 && userCreds[1] == b.Name() {
			authResult, err = doAuth(userCreds[1], password, b.Name(), requested)
		} else {
			//try with empty password
			authResult, err = doAuth(b.Name(), "", b.Name(), requested)
		}

		if err != nil {
			return errors.NewError(err, "Authentication Failed")
		}

		// Auth succeeded
		if authResult == true {
			return nil
		}
		continue

	}

	// no credentials specified
	if len(credentials) == 0 {
		authResult, err = doAuth(b.Name(), "", b.Name(), requested)
	}

	if authResult == false {
		return errors.NewError(nil, "Authentication Failed for bucket "+b.Name())
	}

	return nil
}

func (b *keyspace) CreatePrimaryIndex(using datastore.IndexType) (datastore.PrimaryIndex, errors.Error) {
	if _, exists := b.indexes[PRIMARY_INDEX]; exists {
		return nil, errors.NewError(nil, "Primary index already exists")
	}
	switch using {
	case datastore.VIEW:
		idx, err := newViewPrimaryIndex(b)
		if err != nil {
			return nil, errors.NewError(err, "Error creating primary index")
		}
		b.indexes[idx.Name()] = idx
		return idx, nil

		/*
			case datastore.LSM:
				idx, err := new2iPrimaryIndex(b, using)
				if err != nil {
					return nil, errors.NewError(err, "Error creating primary index")
				}
				logging.Debugf("Created Primary 2i index `%s`", idx.Name())
				b.indexes[idx.Name()] = idx
				return idx, nil
		*/
	default:
		return nil, errors.NewError(nil, "Not yet implemented.")
	}
}

func (b *keyspace) CreateIndex(name string, equalKey, rangeKey expression.Expressions,
	where expression.Expression, using datastore.IndexType) (datastore.Index, errors.Error) {

	if using == "" {
		// current default is VIEW
		using = datastore.VIEW
	}

	if _, exists := b.indexes[name]; exists {
		return nil, errors.NewError(nil, fmt.Sprintf("Index already exists: %s", name))
	}

	// if the name matches any of the unusable indexes, return an error
	for _, iname := range b.nonUsableIndexes {
		if name == iname {
			return nil, errors.NewError(nil, fmt.Sprintf("Index already exists: %s", name))
		}
	}

	switch using {
	case datastore.VIEW:
		idx, err := newViewIndex(name, datastore.IndexKey(rangeKey), where, b)
		if err != nil {
			return nil, errors.NewError(err, fmt.Sprintf("Error creating index: %s", name))
		}
		b.indexes[idx.Name()] = idx
		return idx, nil

		/*
			case datastore.LSM:
				idx, err := new2iIndex(name, equalKey, rangeKey, where, using, b)
				if err != nil {
					return nil, errors.NewError(err, fmt.Sprintf("Error creating index: %s", name))
				}
				logging.Debugf("Created 2i index `%s`", idx.Name())
				b.indexes[idx.Name()] = idx
				return idx, nil
		*/
	default:
		return nil, errors.NewError(nil, "Not yet implemented.")
	}
}

func (b *keyspace) Fetch(keys []string) ([]datastore.AnnotatedPair, errors.Error) {

	if len(keys) == 0 {
		return nil, errors.NewError(nil, "No keys to fetch")
	}

	bulkResponse, err := b.cbbucket.GetBulk(keys)
	if err != nil {
		return nil, errors.NewError(err, "Error doing bulk get")
	}

	i := 0
	rv := make([]datastore.AnnotatedPair, len(bulkResponse))
	for k, v := range bulkResponse {

		var doc datastore.AnnotatedPair
		doc.Key = k

		Value := value.NewAnnotatedValue(value.NewValue(v.Body))

		meta_flags := binary.BigEndian.Uint32(v.Extras[0:4])
		meta_type := "json"
		if Value.Type() == value.BINARY {
			meta_type = "base64"
		}
		Value.SetAttachment("meta", map[string]interface{}{
			"id":    k,
			"cas":   float64(v.Cas),
			"type":  meta_type,
			"flags": float64(meta_flags),
		})

		doc.Value = Value
		rv[i] = doc
		i++

	}

	logging.Debugf("Fetched %d keys ", i)

	return rv, nil
}

const (
	INSERT = 0x01
	UPDATE = 0x02
	UPSERT = 0x04
)

func opToString(op int) string {

	switch op {
	case INSERT:
		return "insert"
	case UPDATE:
		return "update"
	case UPSERT:
		return "upsert"
	}

	return "unknown operation"
}

func (b *keyspace) performOp(op int, inserts []datastore.Pair) ([]datastore.Pair, errors.Error) {

	if len(inserts) == 0 {
		return nil, errors.NewError(nil, "No keys to insert")
	}

	insertedKeys := make([]datastore.Pair, 0)
	var err error

	for _, kv := range inserts {
		key := kv.Key
		value := kv.Value.Actual()

		// TODO Need to also set meta
		switch op {

		case INSERT:
			var added bool
			// add the key to the backend
			added, err = b.cbbucket.Add(key, 0, value)
			if added == false {
				err = errors.NewError(nil, "Key "+key+" Exists")
			}
		case UPDATE:
			// check if the key exists and if so then use the cas value
			// to update the key
			rv := map[string]interface{}{}
			var cas uint64

			err = b.cbbucket.Gets(key, &rv, &cas)
			if err == nil {
				err = b.cbbucket.Set(key, 0, value)
			} else {
				logging.Errorf("Failed to insert. Key exists %s", key)
			}
		case UPSERT:
			err = b.cbbucket.Set(key, 0, value)
		}

		if err != nil {
			logging.Errorf("Failed to perform %s on key %s Error %v", opToString(op), key, err)
		} else {
			insertedKeys = append(insertedKeys, kv)
		}
	}

	if len(insertedKeys) == 0 {
		return nil, errors.NewError(err, "Failed to perform "+opToString(op))
	}

	return insertedKeys, nil

}

func (b *keyspace) Insert(inserts []datastore.Pair) ([]datastore.Pair, errors.Error) {
	return b.performOp(INSERT, inserts)

}

func (b *keyspace) Update(updates []datastore.Pair) ([]datastore.Pair, errors.Error) {
	return b.performOp(UPDATE, updates)
}

func (b *keyspace) Upsert(upserts []datastore.Pair) ([]datastore.Pair, errors.Error) {
	return b.performOp(UPSERT, upserts)
}

func (b *keyspace) Delete(deletes []string) errors.Error {

	failedDeletes := make([]string, 0)
	var err error
	for _, key := range deletes {
		if err = b.cbbucket.Delete(key); err != nil {
			logging.Infof("Failed to delete key %s", key)
			failedDeletes = append(failedDeletes, key)
		}
	}

	if len(failedDeletes) > 0 {
		return errors.NewError(err, "Some keys were not deleted "+fmt.Sprintf("%v", failedDeletes))
	}

	return nil
}

func (b *keyspace) Release() {
	b.deleted = true
	b.cbbucket.Close()
}

func (b *keyspace) loadIndexes() (err errors.Error) {
	if err1 := b.loadViewIndexes(); err1 != nil {
		err = err1
	}
	/*
		if err2 := b.load2iIndexes(); err2 != nil {
			err = err2
		}
	*/
	return
}

// primaryIndex performs full keyspace scans.
type primaryIndex struct {
	viewIndex
}

func (pi *primaryIndex) KeyspaceId() string {
	return pi.keyspace.Id()
}

func (pi *primaryIndex) Id() string {
	return pi.Name()
}

func (pi *primaryIndex) Name() string {
	return pi.name
}

func (pi *primaryIndex) Type() datastore.IndexType {
	return pi.viewIndex.Type()
}

func (pi *primaryIndex) SeekKey() expression.Expressions {
	return nil
}

func (pi *primaryIndex) RangeKey() expression.Expressions {
	// FIXME
	return nil
}

func (pi *primaryIndex) Condition() expression.Expression {
	return nil
}

func (pi *primaryIndex) State() (datastore.IndexState, errors.Error) {
	return pi.viewIndex.State()
}

func (pi *primaryIndex) Statistics(span *datastore.Span) (datastore.Statistics, errors.Error) {
	return pi.viewIndex.Statistics(span)
}

func (pi *primaryIndex) Drop() errors.Error {
	return pi.viewIndex.Drop()
}

func (pi *primaryIndex) Scan(span *datastore.Span, distinct bool, limit int64, conn *datastore.IndexConnection) {
	pi.viewIndex.Scan(span, distinct, limit, conn)
}

func (pi *primaryIndex) ScanEntries(limit int64, conn *datastore.IndexConnection) {
	pi.viewIndex.ScanEntries(limit, conn)
}
