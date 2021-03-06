//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package plan

import (
	"encoding/json"

	"github.com/couchbaselabs/query/algebra"
	"github.com/couchbaselabs/query/expression"
	"github.com/couchbaselabs/query/expression/parser"
)

type Order struct {
	readonly
	terms algebra.SortTerms
}

func NewOrder(order *algebra.Order) *Order {
	return &Order{
		terms: order.Terms(),
	}
}

func (this *Order) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitOrder(this)
}

func (this *Order) New() Operator {
	return &Order{}
}

func (this *Order) Terms() algebra.SortTerms {
	return this.terms
}

func (this *Order) MarshalJSON() ([]byte, error) {
	r := map[string]interface{}{"#operator": "Order"}

	/* generate sort terms */
	s := make([]interface{}, 0, len(this.terms))
	for _, term := range this.terms {
		q := make(map[string]interface{})
		q["expr"] = expression.NewStringer().Visit(term.Expression())

		if term.Descending() {
			q["desc"] = term.Descending()
		}

		s = append(s, q)
	}
	r["sort_terms"] = s
	return json.Marshal(r)
}

func (this *Order) UnmarshalJSON(body []byte) error {
	var _unmarshalled struct {
		_     string `json:"#operator"`
		Terms []struct {
			Expr string `json:"expr"`
			Desc bool   `json:"desc"`
		} `json:"sort_terms"`
	}

	err := json.Unmarshal(body, &_unmarshalled)
	if err != nil {
		return err
	}

	this.terms = make(algebra.SortTerms, len(_unmarshalled.Terms))
	for i, term := range _unmarshalled.Terms {
		expr, err := parser.Parse(term.Expr)
		if err != nil {
			return err
		}
		this.terms[i] = algebra.NewSortTerm(expr, term.Desc)
	}
	return nil
}
