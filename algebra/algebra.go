//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

/*
Package algebra provides a syntax-independent algebra. Any language
flavor or syntax that can be converted to this algebra can then be
processed by the query engine.
*/
package algebra

import (
	"github.com/couchbaselabs/query/datastore"
	"github.com/couchbaselabs/query/errors"
	"github.com/couchbaselabs/query/expression"
	"github.com/couchbaselabs/query/value"
)

/*
The Statement interface represents a N1QL statement, e.g. a SELECT,
UPDATE, or CREATE INDEX statement.
*/
type Statement interface {
	/*
		Visitor pattern.
	*/
	Accept(visitor Visitor) (interface{}, error)

	/*
		The shape of this statement's return values.
	*/
	Signature() value.Value

	/*
		Fully qualify all identifiers in this statement.
	*/
	Formalize() error

	/*
		Apply a Mapper to all the expressions in this statement.
	*/
	MapExpressions(mapper expression.Mapper) error

	/*
		Returns all contained Expressions.
	*/
	Expressions() expression.Expressions

	/*
		Returns all required privileges.
	*/
	Privileges() (datastore.Privileges, errors.Error)
}

/*
The Node interface represents a node in the algebra tree (AST). It is
used internally within the algebra package for polymorphism and
visitor pattern.
*/
type Node interface {
	/*
	   Visitor pattern.
	*/
	Accept(visitor NodeVisitor) (interface{}, error)
}
