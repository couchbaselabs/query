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
	"fmt"

	"github.com/couchbaselabs/query/algebra"
	"github.com/couchbaselabs/query/datastore"
)

func (this *builder) VisitUpsert(stmt *algebra.Upsert) (interface{}, error) {
	ksref := stmt.KeyspaceRef()
	ksref.SetDefaultNamespace(this.namespace)

	keyspace, err := this.getNameKeyspace(ksref.Namespace(), ksref.Keyspace())
	if err != nil {
		return nil, err
	}

	children := make([]Operator, 0, 4)

	creds := this.Credentials()
	auth := NewAuthenticate(keyspace, creds, datastore.CAN_WRITE)
	children = append(this.children, auth)

	if stmt.Values() != nil {
		children = append(children, NewValueScan(stmt.Values()))
	} else if stmt.Select() != nil {
		sel, err := stmt.Select().Accept(this)
		if err != nil {
			return nil, err
		}

		children = append(children, sel.(Operator))
	} else {
		return nil, fmt.Errorf("UPSERT missing both VALUES and SELECT.")
	}

	subChildren := make([]Operator, 0, 4)
	subChildren = append(subChildren, NewSendUpsert(keyspace, stmt.Key()))

	if stmt.Returning() != nil {
		subChildren = append(subChildren, NewInitialProject(stmt.Returning()), NewFinalProject())
	} else {
		subChildren = append(subChildren, NewDiscard())
	}

	parallel := NewParallel(NewSequence(subChildren...))
	children = append(children, parallel)
	return NewSequence(children...), nil
}
