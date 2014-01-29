//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package algebra

import (
	_ "fmt"
	_ "github.com/couchbaselabs/query/value"
)

type Insert struct {
	bucket    *BucketRef           `json:"bucket"`
	keys      Expression           `json:"keys"`
	values    Expression           `json:"values"`
	query     *Select              `json:"query"`
	as        string               `json:"as"`
	returning ResultExpressionList `json:"returning"`
}

func NewInsert(bucket *BucketRef, keys, values Expression, query *Select,
	as string, returning ResultExpressionList) *Insert {
	return &Insert{bucket, keys, values, query, as, returning}
}

func (this *Insert) HandleNode(handler Handler) (interface{}, error) {
	return handler.HandleInsert(this)
}
