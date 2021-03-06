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

	"github.com/couchbaselabs/query/datastore"
)

type Authorize struct {
	readonly
	privs datastore.Privileges `json:"privileges"`
	child Operator             `json:"child"`
}

func NewAuthorize(privs datastore.Privileges, child Operator) *Authorize {
	return &Authorize{
		privs: privs,
		child: child,
	}
}

func (this *Authorize) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitAuthorize(this)
}

func (this *Authorize) New() Operator {
	return &Authorize{}
}

func (this *Authorize) Privileges() datastore.Privileges {
	return this.privs
}

func (this *Authorize) Child() Operator {
	return this.child
}

func (this *Authorize) MarshalJSON() ([]byte, error) {
	r := map[string]interface{}{"#operator": "Authorize"}
	r["privileges"] = this.privs
	r["child"] = this.child
	return json.Marshal(r)
}

func (this *Authorize) UnmarshalJSON(body []byte) error {
	var _unmarshalled struct {
		_     string               `json:"#operator"`
		Privs datastore.Privileges `json:"privileges"`
		Child Operator             `json:"child"`
	}
	err := json.Unmarshal(body, &_unmarshalled)
	this.privs = _unmarshalled.Privs
	this.child = _unmarshalled.Child
	return err
}
