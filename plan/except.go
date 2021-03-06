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
)

type ExceptAll struct {
	readonly
	first  Operator
	second Operator
}

func NewExceptAll(first, second Operator) *ExceptAll {
	return &ExceptAll{
		first:  first,
		second: second,
	}
}

func (this *ExceptAll) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitExceptAll(this)
}

func (this *ExceptAll) New() Operator {
	return &ExceptAll{}
}

func (this *ExceptAll) First() Operator {
	return this.first
}

func (this *ExceptAll) Second() Operator {
	return this.second
}

func (this *ExceptAll) MarshalJSON() ([]byte, error) {
	r := map[string]interface{}{"#operator": "ExceptAll"}
	r["first"] = this.first
	r["second"] = this.second
	return json.Marshal(r)
}

func (this *ExceptAll) UnmarshalJSON(body []byte) error {
	var _unmarshalled struct {
		_      string          `json:"#operator"`
		First  json.RawMessage `json:"first"`
		Second json.RawMessage `json:"second"`
	}

	err := json.Unmarshal(body, &_unmarshalled)
	if err != nil {
		return err
	}

	for i, child := range []json.RawMessage{_unmarshalled.First, _unmarshalled.Second} {
		var op_type struct {
			Operator string `json:"#operator"`
		}

		err = json.Unmarshal(child, &op_type)
		if err != nil {
			return err
		}

		if i == 0 {
			this.first, err = MakeOperator(op_type.Operator, child)
		} else {
			this.second, err = MakeOperator(op_type.Operator, child)
		}
		return err
	}

	return nil
}
