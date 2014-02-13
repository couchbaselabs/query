//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package execute

import (
	"github.com/couchbaselabs/query/plan"
	"github.com/couchbaselabs/query/value"
)

type CountScan struct {
	base
	plan *plan.CountScan
}

func NewCountScan(plan *plan.CountScan) *CountScan {
	rv := &CountScan{
		base: newBase(),
		plan: plan,
	}

	rv.output = rv
	return rv
}

func (this *CountScan) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitCountScan(this)
}

func (this *CountScan) Copy() Operator {
	return &CountScan{this.base.copy(), this.plan}
}

func (this *CountScan) RunOnce(context *Context, parent value.Value) {
	this.once.Do(func() {
		defer close(this.itemChannel) // Broadcast that I have stopped

		count, e := this.plan.Bucket().Count()
		if e != nil {
			context.ErrorChannel() <- e
			return
		}

		cv := value.NewCorrelatedValue(nil, parent)
		av := value.NewAnnotatedValue(cv)
		av.SetAttachment("count", value.NewValue(count))
		this.output.ItemChannel() <- av
	})
}
