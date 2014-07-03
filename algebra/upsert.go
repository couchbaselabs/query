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
	"github.com/couchbaselabs/query/expression"
)

type Upsert struct {
	bucket    *BucketRef             `json:"bucket"`
	key       expression.Expression  `json:"key"`
	values    expression.Expressions `json:"values"`
	query     *Select                `json:"query"`
	as        string                 `json:"as"`
	returning *Projection            `json:"returning"`
}

func NewUpsertValues(bucket *BucketRef, key expression.Expression,
	values expression.Expressions, as string, returning *Projection) *Upsert {
	return &Upsert{
		bucket:    bucket,
		key:       key,
		values:    values,
		query:     nil,
		as:        as,
		returning: returning,
	}
}

func NewUpsertSelect(bucket *BucketRef, key expression.Expression,
	query *Select, as string, returning *Projection) *Upsert {
	return &Upsert{
		bucket:    bucket,
		key:       key,
		values:    nil,
		query:     query,
		as:        as,
		returning: returning,
	}
}

func (this *Upsert) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitUpsert(this)
}
