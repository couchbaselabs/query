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

type Authenticate struct {
	readonly
	keyspace    datastore.Keyspace
	credentials datastore.Credentials
}

func NewAuthenticate(keyspace datastore.Keyspace, creds datastore.Credentials) *Authenticate {
	return &Authenticate{
		keyspace:    keyspace,
		credentials: creds,
	}
}

func (this *Authenticate) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitAuthenticate(this)
}

func (this *Authenticate) MarshalJSON() ([]byte, error) {
	r := map[string]interface{}{"#operator": "Authenticate"}
	r["keyspace"] = this.keyspace.Name()
	if len(this.credentials) > 0 {
		q := make(map[string]interface{})
		for _, cred := range this.credentials {
			q["username"] = cred.Username()
			q["password"], _ = cred.Password()
		}

		r["credentials"] = q
	}
	return json.Marshal(r)
}

func (this *Authenticate) Keyspace() datastore.Keyspace {
	return this.keyspace
}

func (this *Authenticate) GetCredentials() datastore.Credentials {
	return this.credentials
}
