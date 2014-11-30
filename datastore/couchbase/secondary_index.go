// Copyright (c) 2014 Couchbase, Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an "AS IS"
// BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express
// or implied. See the License for the specific language governing
// permissions and limitations under the License.

package couchbase

import "encoding/json"
import "sync"

import c "github.com/couchbase/indexing/secondary/common"
import "github.com/couchbase/indexing/secondary/collatejson"
import protobuf "github.com/couchbase/indexing/secondary/protobuf/query"
import qclient "github.com/couchbase/indexing/secondary/queryport/client"
import "github.com/couchbaselabs/query/datastore"
import "github.com/couchbaselabs/query/errors"
import "github.com/couchbaselabs/query/expression"
import "github.com/couchbaselabs/query/expression/parser"
import "github.com/couchbaselabs/query/value"
import "github.com/couchbaselabs/query/logging"

// ErrorIndexEmpty is index not initialized.
var ErrorIndexEmpty = errors.NewError(nil, "secondaryIndex.empty")

// ErrorEmptyHost is no valid node hosting an index.
var ErrorEmptyHost = errors.NewError(nil, "secondaryIndex.emptyHost")

// ErrorEmptyStatistics is index-statistics not available.
var ErrorEmptyStatistics = errors.NewError(nil, "secondaryIndex.emptyStatistics")

// secondaryIndex to hold meta data information, network-address for
// a single secondary-index.
type secondaryIndex struct {
	name      string // name of the index
	defnID    string
	keySpace  datastore.Keyspace
	isPrimary bool
	using     datastore.IndexType
	partnExpr string
	secExprs  []string
	whereExpr string
	state     datastore.IndexState

	// mutex is used update `stats` and `statBins` fields.
	mu       sync.Mutex
	stats    *statistics
	statBins []*statistics
	// remote node hosting this index.
	hosts       []string
	hostClients []*qclient.Client
}

// TODO: keep upto date with couchbase/indexing/secondary/indexer pkg.
var twoiInclusion = map[datastore.Inclusion]int{
	datastore.NEITHER: 0,
	datastore.LOW:     1,
	datastore.HIGH:    2,
	datastore.BOTH:    3,
}

func (si *secondaryIndex) getHostClient() (*qclient.Client, errors.Error) {
	if si.hostClients == nil || len(si.hostClients) == 0 {
		return nil, ErrorEmptyHost
	}
	// TODO: use round-robin or other statistical heuristics to load balance.
	client := si.hostClients[0]
	return client, nil
}

// KeyspaceId implement Index{} interface.
func (si *secondaryIndex) KeyspaceId() string {
	return si.keySpace.Id()
}

// Id implement Index{} interface.
func (si *secondaryIndex) Id() string {
	return si.Name()
}

// Name implement Index{} interface.
func (si *secondaryIndex) Name() string {
	return si.name
}

// Type implement Index{} interface.
func (si *secondaryIndex) Type() datastore.IndexType {
	return si.using
}

// IsPrimary implement Index{} interface.
func (si *secondaryIndex) IsPrimary() bool {
	return false
}

// EqualKey implement Index{} interface.
func (si *secondaryIndex) EqualKey() expression.Expressions {
	if si != nil && si.partnExpr != "" {
		expr, _ := parser.Parse(si.partnExpr)
		return expression.Expressions{expr}
	}
	return nil
}

// RangeKey implement Index{} interface.
func (si *secondaryIndex) RangeKey() expression.Expressions {
	if si != nil && si.secExprs != nil {
		exprs := make(expression.Expressions, 0, len(si.secExprs))
		for _, exprS := range si.secExprs {
			expr, _ := parser.Parse(exprS)
			exprs = append(exprs, expr)
		}
		return exprs
	}
	return nil
}

// Condition implement Index{} interface.
func (si *secondaryIndex) Condition() expression.Expression {
	if si != nil && si.whereExpr != "" {
		expr, _ := parser.Parse(si.whereExpr)
		return expr
	}
	return nil
}

// State implement Index{} interface.
func (si *secondaryIndex) State() (datastore.IndexState, errors.Error) {
	return si.state, nil
}

// Statistics implement Index{} interface.
func (si *secondaryIndex) Statistics(
	span *datastore.Span) (datastore.Statistics, errors.Error) {

	client, err := si.getHostClient()
	if err != nil {
		return nil, err
	}

	low, high := keys2JSON(span.Range.Low), keys2JSON(span.Range.High)
	equal := [][]byte{keys2JSON(span.Equal)}
	incl := uint32(twoiInclusion[span.Range.Inclusion])
	indexn, bucketn := si.name, si.keySpace.Name()
	pstats, e := client.Statistics(indexn, bucketn, low, high, equal, incl)
	if e != nil {
		return nil, errors.NewError(nil, e.Error())
	}

	si.mu.Lock()
	defer si.mu.Unlock()
	si.stats = (&statistics{}).updateStats(pstats)
	return si.stats, nil
}

// Drop implement Index{} interface.
func (si *secondaryIndex) Drop() errors.Error {
	if si == nil {
		return ErrorIndexEmpty
	}
	client := qclient.NewClusterClient(ClusterManagerAddr)
	err := client.DropIndex(si.defnID)
	// TODO: sync with cluster-manager ?
	if b, ok := si.keySpace.(*keyspace); ok {
		delete(b.indexes, si.Name())
		logging.Infof("Dropped index %v", si.Name())
	}
	if err != nil {
		return errors.NewError(nil, err.Error())
	}
	return nil
}

// Scan implement Index{} interface.
func (si *secondaryIndex) Scan(
	span *datastore.Span, distinct bool, limit int64,
	conn *datastore.IndexConnection) {

	entryChannel := conn.EntryChannel()
	stopChannel := conn.StopChannel()
	defer close(entryChannel)

	client, err := si.getHostClient()
	if err != nil {
		return
	}

	low, high := keys2JSON(span.Range.Low), keys2JSON(span.Range.High)
	equal := [][]byte{keys2JSON(span.Equal)}
	incl := uint32(twoiInclusion[span.Range.Inclusion])
	indexn, bucketn := si.name, si.keySpace.Name()
	client.Scan(
		indexn, bucketn, low, high, equal, incl,
		1 /*page-size*/, distinct, limit,
		func(data interface{}) bool {
			switch val := data.(type) {
			case *protobuf.ResponseStream:
				if err := val.GetErr().GetError(); err != "" {
					conn.Error(errors.NewError(nil, err))
					return false
				}
				for _, entry := range val.GetEntries() {
					// Primary-key is mandatory.
					e := &datastore.IndexEntry{
						PrimaryKey: string(entry.GetPrimaryKey()),
					}
					secKey := entry.GetEntryKey()
					if len(secKey) > 0 {
						key, err := json2Entry(secKey)
						if err != nil {
							conn.Error(errors.NewError(nil, err.Error()))
							return false
						}
						e.EntryKey = value.Values(key)
					}
					select {
					case entryChannel <- e:
					case <-stopChannel:
						return false
					}
				}
				return true

			case error:
				conn.Error(errors.NewError(nil, val.Error()))
				return false
			}
			return false
		})
}

// Scan implement PrimaryIndex{} interface.
func (si *secondaryIndex) ScanEntries(
	limit int64, conn *datastore.IndexConnection) {

	entryChannel := conn.EntryChannel()
	stopChannel := conn.StopChannel()
	defer close(entryChannel)

	client, err := si.getHostClient()
	if err != nil {
		return
	}

	indexn, bucketn := si.name, si.keySpace.Name()
	client.ScanAll(
		indexn, bucketn, 1 /*page-size*/, limit,
		func(data interface{}) bool {
			switch val := data.(type) {
			case *protobuf.ResponseStream:
				if err := val.GetErr().GetError(); err != "" {
					conn.Error(errors.NewError(nil, err))
					return false
				}
				for _, entry := range val.GetEntries() {
					// Primary-key is mandatory.
					e := &datastore.IndexEntry{
						PrimaryKey: string(entry.GetPrimaryKey()),
					}
					secKey := entry.GetEntryKey()
					if len(secKey) > 0 {
						key, err := json2Entry(secKey)
						if err != nil {
							conn.Error(errors.NewError(nil, err.Error()))
							return false
						}
						e.EntryKey = value.Values(key)
					}
					select {
					case entryChannel <- e:
					case <-stopChannel:
						return false
					}
				}
				return true

			case error:
				conn.Error(errors.NewError(nil, val.Error()))
				return false
			}
			return false
		})
}

type statistics struct {
	mu         sync.Mutex
	count      int64
	uniqueKeys int64
	min        []byte // JSON represented min value.Value{}
	max        []byte // JSON represented max value.Value{}
}

// Count implement Statistics{} interface.
func (stats *statistics) Count() (int64, errors.Error) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if stats == nil {
		return 0, ErrorEmptyStatistics
	}
	return stats.count, nil
}

// DistinctCount implement Statistics{} interface.
func (stats *statistics) DistinctCount() (int64, errors.Error) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if stats == nil {
		return 0, ErrorEmptyStatistics
	}
	return stats.uniqueKeys, nil
}

// Min implement Statistics{} interface.
func (stats *statistics) Min() (value.Values, errors.Error) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if stats == nil {
		return nil, ErrorEmptyStatistics
	}
	vals := value.NewValue(stats.min).Actual().([]interface{})
	values := make(value.Values, 0, len(vals))
	for _, val := range vals {
		values = append(values, value.NewValue(val))
	}
	return values, nil
}

// Max implement Statistics{} interface.
func (stats *statistics) Max() (value.Values, errors.Error) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if stats == nil {
		return nil, ErrorEmptyStatistics
	}
	vals := value.NewValue(stats.max).Actual().([]interface{})
	values := make(value.Values, 0, len(vals))
	for _, val := range vals {
		values = append(values, value.NewValue(val))
	}
	return values, nil
}

// Bins implement Statistics{} interface.
func (stats *statistics) Bins() ([]datastore.Statistics, errors.Error) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if stats == nil {
		return nil, ErrorEmptyStatistics
	}
	return nil, nil
}

// local function that can be used to asynchronously update
// meta-data information, host network-address from coordinator
// notifications.

// create a queryport client connected to `host`.
func (si *secondaryIndex) setHost(hosts []string) {
	si.mu.Lock()
	defer si.mu.Unlock()

	si.hosts = hosts
	config := c.SystemConfig.SectionConfig("queryport.client.", true)
	if len(hosts) > 0 {
		si.hostClients = make([]*qclient.Client, 0, len(hosts))
		for _, host := range hosts {
			c := qclient.NewClient(host, config)
			si.hostClients = append(si.hostClients, c)
		}
	}
}

func (stats *statistics) updateStats(pstats *protobuf.IndexStatistics) *statistics {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.count = int64(pstats.GetCount())
	stats.uniqueKeys = int64(pstats.GetUniqueKeys())
	stats.min = pstats.GetMin()
	stats.max = pstats.GetMax()
	return stats
}

// shape of key passed to scan-coordinator (indexer node) is,
//      [key1, key2, ... keyN]
// where N expressions supplied in CREATE INDEX
// to evaluate secondary-key.
func keys2JSON(arg value.Values) []byte {
	if arg == nil {
		return nil
	}
	values := []value.Value(arg)
	arr := value.NewValue(make([]interface{}, len(values)))
	for i, val := range values {
		arr.SetIndex(i, val)
	}
	bin, err := arr.MarshalJSON()
	if err != nil {
		logging.Errorf("unable to marshal %v: %v", arg, err)
	}
	return bin
}

// shape of return key from scan-coordinator is,
//      [key1, key2, ... keyN]
// where N keys where evaluated using N expressions supplied in
// CREATE INDEX.
//
// * Each key will be unmarshalled using json and composed into
//   value.Value{}.
// * Missing key will be composed using NewMissingValue(), btw,
//   `key1` will never be missing.
func json2Entry(data []byte) ([]value.Value, error) {
	arr := []interface{}{}
	err := json.Unmarshal(data, &arr)
	if err != nil {
		return nil, err
	}

	// [key1, key2, ... keyN]
	key := make([]value.Value, len(arr))
	for i := 0; i < len(arr); i++ {
		if s, ok := arr[i].(string); ok && collatejson.MissingLiteral.Equal(s) {
			key[i] = value.NewMissingValue()
		} else {
			key[i] = value.NewValue(arr[i])
		}
	}
	return key, nil
}

// ClusterManagerAddr is temporary hard-coded address for cluster-manager-agent
const ClusterManagerAddr = "localhost:9101"

// IndexerAddr is temporary hard-coded address for indexer node.
const IndexerAddr = "localhost:7000"

// load 2i indexes and remember them as part of keyspace.indexes.
// TODO: pointer to interface is double indirection, have no clue.
func load2iIndexes(b datastore.Keyspace) ([]*datastore.Index, error) {
	indexes := make([]*datastore.Index, 0)
	client := qclient.NewClusterClient(ClusterManagerAddr)
	infos, err := client.List()
	if err != nil {
		return nil, err
	} else if infos == nil { // empty list of indexes
		return nil, nil
	}

	var index datastore.Index

	for _, info := range infos {
		if info.Bucket != b.Name() {
			continue
		}
		using := datastore.IndexType(info.Using)
		if info.Name == "#primary" {
			index, err = new2iPrimaryIndex(b, using, &info)
			if err != nil {
				return nil, err
			}

		} else {
			index, err = new2iIndex(b, &info)
			if err != nil {
				return nil, err
			}
		}
		indexes = append(indexes, &index)
	}
	return indexes, nil
}

// create2iPrimaryIndex will create a new primary index for `keyspace`.
func create2iPrimaryIndex(
	b datastore.Keyspace, using datastore.IndexType) (*secondaryIndex, errors.Error) {

	client := qclient.NewClusterClient(ClusterManagerAddr)
	// update meta-data.
	info, err := client.CreateIndex(
		PRIMARY_INDEX, b.Name(), string(using), "N1QL", "", "", nil, true)
	if err != nil {
		return nil, errors.NewError(err, " Primary CreateIndex() with 2i failed")
	} else if info == nil {
		return nil, errors.NewError(nil, " primary CreateIndex() with 2i failed")
	}
	// TODO: make another call to cluster-manager for topology information,
	// so that info will contain the nodes that host this index.
	return new2iPrimaryIndex(b, using, info)
}

// new2iPrimaryIndex will create a new instance of primary index.
func new2iPrimaryIndex(
	b datastore.Keyspace, using datastore.IndexType,
	info *qclient.IndexInfo) (*secondaryIndex, errors.Error) {

	index := &secondaryIndex{
		name:      PRIMARY_INDEX,
		defnID:    info.DefnID,
		keySpace:  b,
		isPrimary: true,
		using:     datastore.LSM,
		// remote node hosting this index.
		hosts: nil, // to becomputed by coordinator
	}
	// TODO: info will contain the nodes that host this index.
	index.setHost([]string{IndexerAddr})
	return index, nil
}

// create2iIndex will create a new index for `keyspace`.
func create2iIndex(
	name string,
	equalKey, rangeKey expression.Expressions, where expression.Expression,
	using datastore.IndexType,
	b datastore.Keyspace) (*secondaryIndex, errors.Error) {

	var partnStr string
	if equalKey != nil && len(equalKey) > 0 {
		partnStr = expression.NewStringer().Visit(equalKey[0])
	}

	var whereStr string
	if where != nil {
		whereStr = expression.NewStringer().Visit(where)
	}

	secStrs := make([]string, len(rangeKey))
	for i, key := range rangeKey {
		s := expression.NewStringer().Visit(key)
		secStrs[i] = s
	}

	client := qclient.NewClusterClient(ClusterManagerAddr)
	info, err := client.CreateIndex(
		name, b.Name(), string(using), "N1QL", partnStr, whereStr, secStrs, false)
	if err != nil {
		return nil, errors.NewError(nil, err.Error())
	} else if info == nil {
		return nil, errors.NewError(nil, "2i CreateIndex() failed")
	}
	// TODO: make another call to cluster-manager for topology information.
	// so that info will contain the nodes that host this index.
	return new2iIndex(b, info)
}

// new 2i index.
func new2iIndex(
	b datastore.Keyspace,
	info *qclient.IndexInfo) (*secondaryIndex, errors.Error) {

	index := &secondaryIndex{
		name:      info.Name,
		defnID:    info.DefnID,
		keySpace:  b,
		isPrimary: info.IsPrimary,
		using:     datastore.IndexType(info.Using),
		partnExpr: info.PartnExpr,
		secExprs:  info.SecExprs,
		whereExpr: info.WhereExpr,
		// remote node hosting this index.
		hosts: nil, // to becomputed by coordinator
	}
	// TODO: info will contain the nodes that host this index.
	index.setHost([]string{IndexerAddr})
	return index, nil
}

func parseExprs(exprs []string) (expression.Expressions, error) {
	keys := expression.Expressions(nil)
	if len(exprs) > 0 {
		for _, expr := range exprs {
			if len(expr) > 0 {
				key, err := parser.Parse(expr)
				if err != nil {
					return nil, err
				}
				keys = append(keys, key)
			}
		}
	}
	return keys, nil
}
